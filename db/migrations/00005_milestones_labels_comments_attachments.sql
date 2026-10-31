-- +goose Up
CREATE TABLE milestones (
    id            uuid             PRIMARY KEY DEFAULT uuidv7(),
    project_id    uuid             NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name          text             NOT NULL,
    description   text             NOT NULL DEFAULT '',
    target_date   date,
    lead_id       uuid             REFERENCES users (id),
    sort_order    double precision NOT NULL,
    -- NULL: the status is computed from the milestone's issues.
    status_manual text             CHECK (status_manual IN ('planned', 'in_progress', 'done', 'canceled')),
    created_by    uuid             REFERENCES users (id),
    created_at    timestamptz      NOT NULL,
    updated_at    timestamptz      NOT NULL,
    UNIQUE (project_id, id)
);
CREATE UNIQUE INDEX milestones_name ON milestones (project_id, lower(name));

-- Same-project FK; deferred because a move rewrites project and milestone in one transaction.
ALTER TABLE issues ADD COLUMN milestone_id uuid,
    ADD FOREIGN KEY (project_id, milestone_id) REFERENCES milestones (project_id, id)
        ON DELETE SET NULL (milestone_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX issues_milestone ON issues (milestone_id) WHERE deleted_at IS NULL;

-- Workspace labels; a group is a label with is_group, one level deep.
CREATE TABLE labels (
    id          uuid             PRIMARY KEY DEFAULT uuidv7(),
    workspace_id uuid            NOT NULL REFERENCES workspaces (id),
    parent_id   uuid             REFERENCES labels (id),
    is_group    boolean          NOT NULL DEFAULT false,
    exclusive   boolean          NOT NULL DEFAULT false,
    name        text             NOT NULL,
    description text             NOT NULL DEFAULT '',
    color       text             NOT NULL,
    position    double precision NOT NULL,
    created_by  uuid             REFERENCES users (id),
    created_at  timestamptz      NOT NULL,
    updated_at  timestamptz      NOT NULL,
    CHECK (NOT (is_group AND parent_id IS NOT NULL)),
    CHECK (is_group OR NOT exclusive)
);
CREATE UNIQUE INDEX labels_name ON labels (workspace_id, coalesce(parent_id, '00000000-0000-0000-0000-000000000000'), lower(name));
CREATE INDEX labels_parent ON labels (parent_id);

ALTER TABLE workspaces ADD COLUMN type_group_id uuid REFERENCES labels (id),
    ADD COLUMN default_type_label_id uuid REFERENCES labels (id) ON DELETE SET NULL;

-- exclusive_group_id is the label's group when that group is exclusive: at
-- most one label per exclusive group and issue, enforced by the database.
CREATE TABLE issue_labels (
    issue_id           uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    label_id           uuid NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    exclusive_group_id uuid REFERENCES labels (id),
    PRIMARY KEY (issue_id, label_id),
    UNIQUE (issue_id, exclusive_group_id)
);
CREATE INDEX issue_labels_label ON issue_labels (label_id);

CREATE TABLE comments (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    issue_id   uuid        NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    author_id  uuid        NOT NULL REFERENCES users (id),
    body       text        NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    edited_at  timestamptz
);
CREATE INDEX comments_issue ON comments (issue_id, created_at, id);

-- File contents by SHA-256; attachments reference them (deduplicated).
CREATE TABLE blobs (
    sha256       bytea       PRIMARY KEY,
    size         bigint      NOT NULL,
    content_type text        NOT NULL,
    created_at   timestamptz NOT NULL
);
-- issue_id NULL: uploaded before its issue or comment was saved (pending).
CREATE TABLE attachments (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    issue_id    uuid        REFERENCES issues (id) ON DELETE CASCADE,
    uploader_id uuid        NOT NULL REFERENCES users (id),
    blob_sha256 bytea       NOT NULL REFERENCES blobs (sha256),
    name        text        NOT NULL,
    created_at  timestamptz NOT NULL
);
CREATE INDEX attachments_issue ON attachments (issue_id, created_at, id);
CREATE INDEX attachments_pending ON attachments (created_at) WHERE issue_id IS NULL;
CREATE INDEX attachments_blob ON attachments (blob_sha256);

ALTER TABLE audit_events DROP CONSTRAINT audit_events_entity_type_check,
    ADD CONSTRAINT audit_events_entity_type_check
        CHECK (entity_type IN ('project', 'state', 'issue', 'milestone', 'label', 'comment', 'attachment'));

-- Seed the Type group for an existing workspace; keep in sync with SeedTypeLabels.
WITH ws AS (
    SELECT id FROM workspaces
), g AS (
    INSERT INTO labels (workspace_id, is_group, exclusive, name, color, position, created_at, updated_at)
    SELECT ws.id, true, true, 'Type', '#6e7781', 0, now(), now() FROM ws
    RETURNING id, workspace_id
), l AS (
    INSERT INTO labels (workspace_id, parent_id, name, color, position, created_at, updated_at)
    SELECT g.workspace_id, g.id, v.name, v.color, v.pos, now(), now()
    FROM g, (VALUES ('Bug', '#e5484d', 0), ('Feature', '#8e4ec6', 1), ('Task', '#6e7781', 2), ('Improvement', '#30a46c', 3)) AS v (name, color, pos)
    RETURNING id, workspace_id, name
)
UPDATE workspaces w
SET type_group_id = g.id,
    default_type_label_id = (SELECT l.id FROM l WHERE l.workspace_id = w.id AND l.name = 'Task')
FROM g
WHERE g.workspace_id = w.id;

-- +goose Down
ALTER TABLE audit_events DROP CONSTRAINT audit_events_entity_type_check;
DELETE FROM audit_events WHERE entity_type IN ('milestone', 'label', 'comment', 'attachment');
ALTER TABLE audit_events ADD CONSTRAINT audit_events_entity_type_check
    CHECK (entity_type IN ('project', 'state', 'issue'));
DROP TABLE attachments;
DROP TABLE blobs;
DROP TABLE comments;
DROP TABLE issue_labels;
ALTER TABLE workspaces DROP COLUMN default_type_label_id, DROP COLUMN type_group_id;
DROP TABLE labels;
DROP INDEX issues_milestone;
ALTER TABLE issues DROP COLUMN milestone_id;
DROP TABLE milestones;
