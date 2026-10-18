-- name: CreateLink :one
INSERT INTO event_link (
  id, from_event_id, to_event_id, kind, created_at, author
) VALUES (
  ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetLink :one
SELECT * FROM event_link
WHERE id = ?;

-- name: GetLinkBetween :one
SELECT * FROM event_link
WHERE from_event_id = ? AND to_event_id = ? AND kind = ?;

-- name: ListEventLinks :many
SELECT * FROM event_link
WHERE from_event_id = sqlc.arg(event_id) OR to_event_id = sqlc.arg(event_id)
ORDER BY created_at, id;

-- name: ListTimelineLinks :many
SELECT * FROM event_link
WHERE from_event_id IN (SELECT event.id FROM event WHERE event.timeline_id = ?)
ORDER BY created_at, id;

-- name: DeleteLink :exec
DELETE FROM event_link
WHERE id = ?;
