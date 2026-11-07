-- +goose Up
CREATE TABLE webhooks (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    workspace_id    uuid        NOT NULL REFERENCES workspaces (id),
    name            text        NOT NULL DEFAULT '',
    url             text        NOT NULL,
    -- Plain text: it is the HMAC key of every delivery.
    secret          text        NOT NULL,
    -- NULL: all projects.
    project_id      uuid        REFERENCES projects (id) ON DELETE CASCADE,
    resources       text[]      NOT NULL
                    CHECK (cardinality(resources) > 0 AND resources <@ ARRAY['issue', 'comment', 'label', 'milestone']),
    enabled         boolean     NOT NULL DEFAULT true,
    -- Set (with disabled_at) only when the webhook was disabled automatically.
    disabled_reason text        CHECK (disabled_reason IN ('failures')),
    disabled_at     timestamptz,
    created_by      uuid        REFERENCES users (id),
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);
CREATE INDEX webhooks_workspace_enabled ON webhooks (workspace_id, enabled);

CREATE TABLE webhook_deliveries (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id      uuid        NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    resource        text        NOT NULL,
    action          text        NOT NULL,
    -- NULL for a test delivery.
    entity_id       uuid,
    -- The envelope without webhookTimestamp.
    payload         jsonb       NOT NULL,
    status          text        NOT NULL CHECK (status IN ('pending', 'retrying', 'succeeded', 'failed', 'canceled')),
    attempts        integer     NOT NULL DEFAULT 0,
    response_status integer,
    last_error      text,
    duration_ms     integer,
    created_at      timestamptz NOT NULL,
    last_attempt_at timestamptz,
    next_attempt_at timestamptz,
    finished_at     timestamptz
);
CREATE INDEX webhook_deliveries_webhook ON webhook_deliveries (webhook_id, created_at DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
