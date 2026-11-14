package attachment_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
)

// stmtTracer is a pgx tracer that counts SQL statements and reports each
// finished statement's SQL to ended, when set.
type stmtTracer struct {
	n     atomic.Int64
	ended func(sql string)
}

type sqlKey struct{}

func (c *stmtTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return context.WithValue(ctx, sqlKey{}, data.SQL)
}

func (c *stmtTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if c.ended != nil {
		sql, _ := ctx.Value(sqlKey{}).(string)
		c.ended(sql)
	}
}

// tracedAttachments returns an attachment service on a second pool over the
// same database and store whose statements go through tracer.
func (e env) tracedAttachments(t *testing.T, tracer pgx.QueryTracer, locks *blob.Locks) *attachment.Service {
	t.Helper()
	cfg := e.pool.Config()
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return attachment.New(pool, e.issues, e.store, locks, 1<<20, e.clock.now)
}

// Stored files that have a blob row cost no statement of their own: only
// files without a row are checked one by one.
func TestCleanupChecksStoredFilesInBatches(t *testing.T) {
	// extra is the fewest statements of a Cleanup over n referenced and n
	// orphan old files, less one per orphan.
	extra := func(n int) int64 {
		e := newEnv(t)
		ctx := context.Background()
		counter := &stmtTracer{}
		svc := e.tracedAttachments(t, counter, &blob.Locks{})
		var least int64 = -1
		for run := range 3 {
			old := e.clock.now().Add(-72 * time.Hour)
			var referenced, orphans []string
			for i := range n {
				data := fmt.Sprintf("referenced %d/%d/%d", n, run, i)
				it := e.issue(t, issue.CreateInput{})
				e.upload(t, e.users.Member, "a.bin", data, &it.ID)
				referenced = append(referenced, e.blobPath(data))
				orphans = append(orphans, e.blobPath(fmt.Sprintf("orphan %d/%d/%d", n, run, i)))
			}
			for _, path := range slices.Concat(referenced, orphans) {
				touch(t, path, old)
			}
			before := counter.n.Load()
			r, err := svc.Cleanup(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if used := counter.n.Load() - before; least < 0 || used < least {
				least = used
			}
			if r.Files != n {
				t.Errorf("%d+%d files, run %d: removed %d, want %d", n, n, run, r.Files, n)
			}
			for _, path := range orphans {
				if exists(path) {
					t.Errorf("orphan file kept: %s", path)
				}
			}
			for _, path := range referenced {
				if !exists(path) {
					t.Errorf("referenced file removed: %s", path)
				}
			}
		}
		return least - int64(n)
	}
	two, ten := extra(2), extra(10)
	if two != ten {
		t.Errorf("statements besides one per orphan: 2+2 files = %d, 10+10 files = %d; want equal", two, ten)
	}
}

// Control: more stale files than one batch holds are all checked.
func TestCleanupSweepsMoreFilesThanOneBatch(t *testing.T) {
	e := newEnv(t)
	keep := e.keepBlobRow(t)
	old := e.clock.now().Add(-72 * time.Hour)
	touch(t, keep, old)
	var orphans []string
	for i := range 1001 {
		path := e.blobPath(fmt.Sprintf("orphan %d", i))
		touch(t, path, old)
		orphans = append(orphans, path)
	}
	r, err := e.attachments.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != len(orphans) {
		t.Errorf("removed %d files, want %d", r.Files, len(orphans))
	}
	for _, path := range orphans {
		if exists(path) {
			t.Fatalf("orphan file kept: %s", path)
		}
	}
	if !exists(keep) {
		t.Error("referenced file removed")
	}
}

// A file found without a row is kept when its row appears before Cleanup
// takes its hash lock: an upload commits its file before inserting the row.
func TestCleanupKeepsFileWhoseRowAppearsBeforeTheLock(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old := e.clock.now().Add(-72 * time.Hour)
	touch(t, e.keepBlobRow(t), old)
	const data = "late row"
	path := e.blobPath(data)
	touch(t, path, old)
	sum := sha256.Sum256([]byte(data))

	read := make(chan struct{}, 1)
	tracer := &stmtTracer{ended: func(sql string) {
		if strings.Contains(sql, "-- name: ExistingBlobs ") {
			select {
			case read <- struct{}{}:
			default:
			}
		}
	}}
	locks := &blob.Locks{}
	svc := e.tracedAttachments(t, tracer, locks)
	unlock := sync.OnceFunc(locks.Lock(sum[:]))
	defer unlock()

	type result struct {
		r   attachment.CleanupResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := svc.Cleanup(ctx)
		done <- result{r, err}
	}()
	select {
	case <-read:
	case <-time.After(10 * time.Second):
		unlock()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		t.Fatal("Cleanup did not read the blob rows of the stored files")
	}
	// The upload's row lands after the batch read, before the lock is free.
	if _, err := e.pool.Exec(ctx, `INSERT INTO blobs (sha256, size, content_type, created_at) VALUES ($1, $2, 'application/octet-stream', $3)`,
		sum[:], len(data), e.clock.now()); err != nil {
		t.Fatal(err)
	}
	unlock()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.r.Files != 0 || !exists(path) {
			t.Errorf("file whose row appeared before the lock: exists %v, result %+v", exists(path), res.r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Cleanup did not finish after the lock was released")
	}
}
