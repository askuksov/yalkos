-- +goose Up
-- One workspace per instance (spec §2.1).
CREATE UNIQUE INDEX workspaces_singleton ON workspaces ((true));
ALTER TABLE workspaces ALTER COLUMN id SET DEFAULT uuidv7();

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT uuidv7(),
    workspace_id  uuid        NOT NULL REFERENCES workspaces (id),
    email         text        NOT NULL,
    name          text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    disabled      boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower ON users (lower(email));
CREATE UNIQUE INDEX users_one_owner ON users ((true)) WHERE role = 'owner';

CREATE TABLE sessions (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE invites (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    token_hash bytea       NOT NULL UNIQUE,
    role       text        NOT NULL CHECK (role IN ('admin', 'member')),
    created_by uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    used_by    uuid        REFERENCES users (id),
    revoked_at timestamptz
);

CREATE TABLE api_keys (
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name           text        NOT NULL,
    key_hash       bytea       NOT NULL UNIQUE,
    display_prefix text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_used_at   timestamptz,
    revoked_at     timestamptz
);
CREATE INDEX api_keys_user ON api_keys (user_id);

-- +goose Down
DROP TABLE api_keys;
DROP TABLE invites;
DROP TABLE sessions;
DROP TABLE users;
ALTER TABLE workspaces ALTER COLUMN id SET DEFAULT gen_random_uuid();
DROP INDEX workspaces_singleton;
