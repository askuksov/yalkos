-- +goose Up
-- One row per source instance an import has read from.
CREATE TABLE import_sources (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    kind         text        NOT NULL CHECK (kind IN ('plane')),
    source_id    text        NOT NULL,
    url          text        NOT NULL,
    workspace    text        NOT NULL,
    first_run_at timestamptz NOT NULL,
    last_run_at  timestamptz NOT NULL,
    UNIQUE (kind, source_id)
);

-- What was imported from where. target_id has no foreign key: it points into
-- several tables, and an entity deleted in Yalkos stays mapped so a re-run
-- does not bring it back.
CREATE TABLE import_map (
    source_id         uuid        NOT NULL REFERENCES import_sources (id),
    kind              text        NOT NULL CHECK (kind IN ('user', 'project', 'state', 'label', 'milestone', 'issue', 'comment', 'attachment')),
    plane_id          text        NOT NULL,
    target_id         uuid        NOT NULL,
    source_updated_at timestamptz,
    imported_at       timestamptz NOT NULL,
    PRIMARY KEY (source_id, kind, plane_id)
);
CREATE INDEX import_map_target ON import_map (kind, target_id);

-- +goose Down
DROP TABLE import_map;
DROP TABLE import_sources;
