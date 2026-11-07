package store_test

import (
	"context"
	"testing"
)

const insertWebhookNoReturn = `INSERT INTO webhooks (workspace_id, url, secret, project_id, resources, enabled, disabled_reason, created_at, updated_at)
	VALUES ($1, 'https://x.example.com/', 's', $2, $3, true, $4, now(), now())`

const insertWebhookSQL = insertWebhookNoReturn + ` RETURNING id::text`

const insertDeliverySQL = `INSERT INTO webhook_deliveries (webhook_id, resource, action, payload, status, created_at)
	VALUES ($1, 'issue', 'create', '{}', $2, now()) RETURNING id::text`

func TestWebhookResourcesCheck(t *testing.T) {
	pool := migrated(t)
	ctx := context.Background()
	ws := insertWorkspace(t, pool)

	// Positive control: a non-empty subset of the four resources is accepted.
	scanID(t, pool, insertWebhookSQL, ws, nil, []string{"issue", "comment", "label", "milestone"}, nil)

	for name, resources := range map[string][]string{
		"empty":   {},
		"unknown": {"issue", "project"},
		"test":    {"test"},
	} {
		_, err := pool.Exec(ctx, insertWebhookNoReturn, ws, nil, resources, nil)
		if err == nil {
			t.Errorf("%s resources accepted", name)
			continue
		}
		wantPgCode(t, err, "23514")
	}
}

func TestWebhookDisabledReasonCheck(t *testing.T) {
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	scanID(t, pool, insertWebhookSQL, ws, nil, []string{"issue"}, "failures")
	scanID(t, pool, insertWebhookSQL, ws, nil, []string{"issue"}, nil)
	_, err := pool.Exec(context.Background(), `INSERT INTO webhooks (workspace_id, url, secret, resources, disabled_reason, created_at, updated_at)
		VALUES ($1, 'https://x.example.com/', 's', '{issue}', 'manual', now(), now())`, ws)
	wantPgCode(t, err, "23514")
}

func TestWebhookDeliveryStatusCheck(t *testing.T) {
	pool := migrated(t)
	ws := insertWorkspace(t, pool)
	hook := scanID(t, pool, insertWebhookSQL, ws, nil, []string{"issue"}, nil)
	for _, status := range []string{"pending", "retrying", "succeeded", "failed", "canceled"} {
		scanID(t, pool, insertDeliverySQL, hook, status)
	}
	var id string
	err := pool.QueryRow(context.Background(), insertDeliverySQL, hook, "sent").Scan(&id)
	wantPgCode(t, err, "23514")
}

func TestWebhookCascades(t *testing.T) {
	pool := migrated(t)
	ctx := context.Background()
	ws := insertWorkspace(t, pool)
	p := insertProject(t, pool, ws, "P")
	scoped := scanID(t, pool, insertWebhookSQL, ws, p, []string{"issue"}, nil)
	global := scanID(t, pool, insertWebhookSQL, ws, nil, []string{"issue"}, nil)
	scanID(t, pool, insertDeliverySQL, scoped, "pending")
	scanID(t, pool, insertDeliverySQL, global, "pending")

	count := func(sql, id string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, p); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM webhooks WHERE id = $1`, scoped); n != 0 {
		t.Errorf("scoped webhook survived its project: %d", n)
	}
	if n := count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = $1`, scoped); n != 0 {
		t.Errorf("deliveries of the scoped webhook survived: %d", n)
	}
	if n := count(`SELECT count(*) FROM webhooks WHERE id = $1`, global); n != 1 {
		t.Errorf("all-projects webhook count = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = $1`, global); n != 1 {
		t.Errorf("deliveries of the all-projects webhook = %d, want 1", n)
	}
}
