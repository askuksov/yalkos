-- +goose Up
-- Full-text search over title (weight A) and description (weight B). The
-- simple config does no stemming: content mixes English and Russian. An
-- expression index instead of a stored column keeps the issues row (and every
-- query reading it) unchanged; queries must repeat this exact expression.
CREATE INDEX issues_search ON issues USING gin ((
  setweight(to_tsvector('simple', title), 'A') || setweight(to_tsvector('simple', description), 'B')
));

-- +goose Down
DROP INDEX issues_search;
