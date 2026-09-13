-- name: AddEventTag :exec
INSERT INTO event_tag (
  event_id, tag
) VALUES (
  ?, ?
)
ON CONFLICT DO NOTHING;

-- name: ListEventTags :many
SELECT tag FROM event_tag
WHERE event_id = ?
ORDER BY tag;

-- name: DeleteEventTag :exec
DELETE FROM event_tag
WHERE event_id = ? AND tag = ?;
