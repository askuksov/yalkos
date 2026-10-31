package blob_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/blob"
)

func put(t *testing.T, s *blob.LocalFS, data string) blob.Staged {
	t.Helper()
	st, err := s.Put(context.Background(), strings.NewReader(data), 1<<20)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return st
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func TestLocalFSPutDedup(t *testing.T) {
	dir := t.TempDir()
	s, err := blob.NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sum := sha256.Sum256([]byte("hello"))
	hexSum := hex.EncodeToString(sum[:])
	for range 2 {
		st := put(t, s, "hello")
		if !bytes.Equal(st.Sum, sum[:]) || st.Size != 5 || string(st.Head) != "hello" {
			t.Fatalf("staged = %+v", st)
		}
		if err := st.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join("blobs", hexSum[:2], hexSum)
	if got := files(t, dir); len(got) != 1 || got[0] != want {
		t.Fatalf("files = %v, want [%s]", got, want)
	}
	r, err := s.Open(ctx, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "hello" {
		t.Errorf("read %q", b)
	}
	if info, err := os.Stat(filepath.Join(dir, "blobs")); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("blobs dir mode = %v, %v", info.Mode().Perm(), err)
	}
	if err := s.Delete(ctx, sum[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, sum[:]); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("open after delete = %v", err)
	}
	if err := s.Delete(ctx, sum[:]); err != nil {
		t.Errorf("deleting a missing blob: %v", err)
	}
}

func TestLocalFSDiscardAndLimit(t *testing.T) {
	dir := t.TempDir()
	s, err := blob.NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	put(t, s, "x").Discard()
	if _, err := s.Put(context.Background(), strings.NewReader("too long"), 3); !errors.Is(err, blob.ErrTooLarge) {
		t.Errorf("over limit = %v, want ErrTooLarge", err)
	}
	if got := files(t, dir); len(got) != 0 {
		t.Errorf("leftover files = %v", got)
	}
}

func TestLocksSameHash(t *testing.T) {
	var l blob.Locks
	unlock := l.Lock([]byte{1, 2, 3})
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Lock([]byte{1, 2, 3})()
	}()
	select {
	case <-done:
		t.Fatal("second lock of the same hash did not wait")
	default:
	}
	unlock()
	<-done
}
