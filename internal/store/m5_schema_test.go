package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/testdb"
)

// wantTypeSeed checks the Type group, its labels in order and the default.
func wantTypeSeed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var groupName, groupColor string
	var exclusive, isGroup bool
	if err := pool.QueryRow(ctx, `SELECT l.name, l.color, l.exclusive, l.is_group FROM workspaces w
		JOIN labels l ON l.id = w.type_group_id`).Scan(&groupName, &groupColor, &exclusive, &isGroup); err != nil {
		t.Fatalf("type group: %v", err)
	}
	if groupName != "Type" || groupColor != "#6e7781" || !exclusive || !isGroup {
		t.Errorf("group = %q %q exclusive=%v isGroup=%v", groupName, groupColor, exclusive, isGroup)
	}
	rows, err := pool.Query(ctx, `SELECT l.name, l.color FROM labels l JOIN workspaces w ON l.parent_id = w.type_group_id
		ORDER BY l.position`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var n, c string
		err := r.Scan(&n, &c)
		return n + " " + c, err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bug #e5484d", "Feature #8e4ec6", "Task #6e7781", "Improvement #30a46c"}
	if len(got) != len(want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("labels[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	var def string
	if err := pool.QueryRow(ctx, `SELECT l.name FROM workspaces w JOIN labels l ON l.id = w.default_type_label_id`).Scan(&def); err != nil {
		t.Fatalf("default type: %v", err)
	}
	if def != "Task" {
		t.Errorf("default = %q, want Task", def)
	}
}

func TestSeedTypeLabels(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)
	p, err := goose.NewProvider(goose.DialectPostgres, stdlib.OpenDBFromPool(pool), db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	// A workspace that exists before the labels migration gets the seed from it.
	if _, err := p.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	insertWorkspace(t, pool)
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	wantTypeSeed(t, pool)
}

func TestSeedTypeLabelsEmptyDB(t *testing.T) {
	pool := migrated(t)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM labels`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("labels without a workspace = %d, want 0", n)
	}
}

// labelFixture is a workspace with a project P (state, issue) and an exclusive group G with labels A, B.
type labelFixture struct {
	schemaFixture
	issue, group, a, b string
}

func newLabelFixture(t *testing.T) labelFixture {
	t.Helper()
	f := labelFixture{schemaFixture: newSchemaFixture(t)}
	ws := scanID(t, f.pool, `SELECT id::text FROM workspaces`)
	f.issue = scanID(t, f.pool, insertIssueSQL, f.p, 1, f.pState, 0, f.user, nil)
	f.group = scanID(t, f.pool, `INSERT INTO labels (workspace_id, is_group, exclusive, name, color, position, created_at, updated_at)
		VALUES ($1, true, true, 'G', '#000000', 0, now(), now()) RETURNING id::text`, ws)
	insLabel := `INSERT INTO labels (workspace_id, parent_id, name, color, position, created_at, updated_at)
		VALUES ($1, $2, $3, '#000000', 0, now(), now()) RETURNING id::text`
	f.a = scanID(t, f.pool, insLabel, ws, f.group, "A")
	f.b = scanID(t, f.pool, insLabel, ws, f.group, "B")
	return f
}

func TestOneLabelPerExclusiveGroup(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	ins := `INSERT INTO issue_labels (issue_id, label_id, exclusive_group_id) VALUES ($1, $2, $3)`
	if _, err := f.pool.Exec(ctx, ins, f.issue, f.a, f.group); err != nil {
		t.Fatal(err)
	}
	_, err := f.pool.Exec(ctx, ins, f.issue, f.b, f.group)
	wantPgCode(t, err, "23505")
}

func TestGroupCannotHaveParent(t *testing.T) {
	f := newLabelFixture(t)
	_, err := f.pool.Exec(context.Background(), `INSERT INTO labels (workspace_id, parent_id, is_group, name, color, position, created_at, updated_at)
		SELECT id, $1, true, 'Nested', '#000000', 0, now(), now() FROM workspaces`, f.group)
	wantPgCode(t, err, "23514")
}

func insertMilestone(t *testing.T, pool *pgxpool.Pool, project, name string) string {
	t.Helper()
	return scanID(t, pool, `INSERT INTO milestones (project_id, name, sort_order, created_at, updated_at)
		VALUES ($1, $2, 1000, now(), now()) RETURNING id::text`, project, name)
}

func TestMilestoneOtherProject(t *testing.T) {
	f := newSchemaFixture(t)
	ctx := context.Background()
	m := insertMilestone(t, f.pool, f.q, "Q1")
	issue := scanID(t, f.pool, insertIssueSQL, f.p, 1, f.pState, 0, f.user, nil)
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		// Deferred FK: the update succeeds, COMMIT fails.
		if _, err := tx.Exec(ctx, `UPDATE issues SET milestone_id = $1 WHERE id = $2`, m, issue); err != nil {
			t.Fatalf("update before commit: %v", err)
		}
		return nil
	})
	wantPgCode(t, err, "23503")
}

func TestDeleteMilestoneClearsIssues(t *testing.T) {
	f := newSchemaFixture(t)
	ctx := context.Background()
	m := insertMilestone(t, f.pool, f.p, "P1")
	issue := scanID(t, f.pool, insertIssueSQL, f.p, 1, f.pState, 0, f.user, nil)
	if _, err := f.pool.Exec(ctx, `UPDATE issues SET milestone_id = $1 WHERE id = $2`, m, issue); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM milestones WHERE id = $1`, m); err != nil {
		t.Fatal(err)
	}
	var milestone *string
	var project string
	if err := f.pool.QueryRow(ctx, `SELECT milestone_id::text, project_id::text FROM issues WHERE id = $1`, issue).
		Scan(&milestone, &project); err != nil {
		t.Fatal(err)
	}
	if milestone != nil || project != f.p {
		t.Errorf("milestone = %v, project = %s (want nil, %s)", milestone, project, f.p)
	}
}

func TestAuditAcceptsNewEntities(t *testing.T) {
	f := newSchemaFixture(t)
	for _, et := range []string{"milestone", "label", "comment", "attachment"} {
		if _, err := f.pool.Exec(context.Background(), `INSERT INTO audit_events (entity_type, entity_id, action, created_at)
			VALUES ($1, uuidv7(), 'create', now())`, et); err != nil {
			t.Errorf("entity %s: %v", et, err)
		}
	}
}
