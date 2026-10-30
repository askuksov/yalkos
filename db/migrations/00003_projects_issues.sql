-- +goose Up
CREATE TABLE projects (
    id               uuid        PRIMARY KEY DEFAULT uuidv7(),
    workspace_id     uuid        NOT NULL REFERENCES workspaces (id),
    key              text        NOT NULL UNIQUE,
    name             text        NOT NULL,
    description      text        NOT NULL DEFAULT '',
    icon             text,
    color            text,
    sequence_counter integer     NOT NULL DEFAULT 0,
    created_by       uuid        REFERENCES users (id),
    archived_at      timestamptz,
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL
);

-- Every key a project has had: former KEY-N identifiers keep resolving (spec §2.2).
CREATE TABLE project_keys (
    key        text PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE
);
CREATE INDEX project_keys_project ON project_keys (project_id);

CREATE TABLE project_members (
    project_id uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE states (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text        NOT NULL,
    type       text        NOT NULL CHECK (type IN ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
    color      text        NOT NULL,
    position   integer     NOT NULL,
    is_default boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (project_id, id)
);
CREATE UNIQUE INDEX states_name ON states (project_id, lower(name));
CREATE UNIQUE INDEX states_one_default ON states (project_id) WHERE is_default;

CREATE TABLE issues (
    id           uuid             PRIMARY KEY DEFAULT uuidv7(),
    project_id   uuid             NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    number       integer          NOT NULL,
    title        text             NOT NULL,
    description  text             NOT NULL DEFAULT '',
    state_id     uuid             NOT NULL,
    priority     smallint         NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    assignee_id  uuid             REFERENCES users (id),
    creator_id   uuid             NOT NULL REFERENCES users (id),
    parent_id    uuid             CHECK (parent_id <> id),
    due_date     date,
    sort_order   double precision NOT NULL,
    created_at   timestamptz      NOT NULL,
    updated_at   timestamptz      NOT NULL,
    completed_at timestamptz,
    canceled_at  timestamptz,
    deleted_at   timestamptz,
    UNIQUE (project_id, number),
    UNIQUE (project_id, id),
    FOREIGN KEY (project_id, state_id) REFERENCES states (project_id, id),
    -- Deferred: a move rewrites parent and children in one transaction.
    FOREIGN KEY (project_id, parent_id) REFERENCES issues (project_id, id)
        ON DELETE SET NULL (parent_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX issues_project ON issues (project_id, sort_order, id) WHERE deleted_at IS NULL;
CREATE INDEX issues_assignee ON issues (assignee_id) WHERE deleted_at IS NULL;
CREATE INDEX issues_parent ON issues (parent_id) WHERE deleted_at IS NULL;
CREATE INDEX issues_state ON issues (state_id);

-- Every number an issue has had (moves): old identifiers keep resolving.
CREATE TABLE issue_numbers (
    project_id uuid    NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    number     integer NOT NULL,
    issue_id   uuid    NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, number)
);
CREATE INDEX issue_numbers_issue ON issue_numbers (issue_id);

CREATE TABLE audit_events (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    entity_type text        NOT NULL CHECK (entity_type IN ('project', 'state', 'issue')),
    entity_id   uuid        NOT NULL,
    project_id  uuid, -- no FK: the record of a deleted project survives
    actor_id    uuid        REFERENCES users (id),
    action      text        NOT NULL CHECK (action IN ('create', 'update', 'delete')),
    changes     jsonb       NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL
);
CREATE INDEX audit_events_entity ON audit_events (entity_id, created_at, id);

-- +goose Down
DROP TABLE audit_events;
DROP TABLE issue_numbers;
DROP TABLE issues;
DROP TABLE states;
DROP TABLE project_members;
DROP TABLE project_keys;
DROP TABLE projects;
