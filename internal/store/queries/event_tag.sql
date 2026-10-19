-- name: AddEventTag :exec
-- An existing tag is left as is.
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

-- name: DeleteEventTags :exec
DELETE FROM event_tag
WHERE event_id = ?;
