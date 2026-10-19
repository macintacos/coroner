-- name: AddTimelineRef :exec
-- An existing ref is left as is.
INSERT INTO timeline_ref (
  timeline_id, ref
) VALUES (
  ?, ?
)
ON CONFLICT DO NOTHING;

-- name: ListTimelineRefs :many
-- ListTimelineRefs returns refs in the order they were added.
SELECT ref FROM timeline_ref
WHERE timeline_id = ?
ORDER BY position;

-- name: DeleteTimelineRefs :exec
DELETE FROM timeline_ref
WHERE timeline_id = ?;
