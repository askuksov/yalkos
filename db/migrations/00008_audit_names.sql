-- +goose Up
-- Names of the labels and milestones an issue change refers to, as they were
-- when it was recorded: {"<id>": {"name": "...", "type": true}}; "type" is
-- present (true) only for labels of the type group. NULL when the change names
-- none, and for rows recorded before this column existed.
ALTER TABLE audit_events ADD COLUMN names jsonb;

-- +goose Down
ALTER TABLE audit_events DROP COLUMN names;
