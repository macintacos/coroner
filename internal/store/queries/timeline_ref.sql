-- name: AddTimelineRef :exec
INSERT INTO timeline_ref (
  timeline_id, ref
) VALUES (
  ?, ?
)
ON CONFLICT DO NOTHING;

-- name: ListTimelineRefs :many
SELECT ref FROM timeline_ref
WHERE timeline_id = ?
ORDER BY rowid;

-- name: DeleteTimelineRef :exec
DELETE FROM timeline_ref
WHERE timeline_id = ? AND ref = ?;
