-- name: GetIssueForPayload :one
-- Reads for webhook payloads: an issue by id, a soft-deleted one included (its
-- removal is reported while the row is still there), with its project's
-- current key and name. Users, states, labels and milestones come from
-- ListUsersByIDs, ListStatesByIDs, ListLabelsByIDs and ListMilestonesByIDs.
SELECT sqlc.embed(issues), p.key AS project_key, p.name AS project_name
FROM issues JOIN projects p ON p.id = issues.project_id
WHERE issues.id = $1;
