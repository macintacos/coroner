-- name: CreateEvent :one
INSERT INTO event (
  id, timeline_id, seq, occurred_at, occurred_offset, occurred_precision, approximate,
  occurred_raw, ended_at, source_id, text, url, author, created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetEvent :one
SELECT * FROM event
WHERE id = ?;

-- name: GetEventBySeq :one
SELECT * FROM event
WHERE timeline_id = ? AND seq = ?;

-- name: ListEventsByIDPrefix :many
SELECT * FROM event
WHERE id GLOB ?
ORDER BY id
LIMIT ?;

-- name: ListEvents :many
SELECT * FROM event
WHERE timeline_id = ?
ORDER BY occurred_at, seq;

-- name: UpdateEvent :one
UPDATE event
SET occurred_at = ?,
  occurred_offset = ?,
  occurred_precision = ?,
  approximate = ?,
  occurred_raw = ?,
  ended_at = ?,
  source_id = ?,
  text = ?,
  url = ?,
  author = ?,
  updated_at = ?
WHERE id = ?
RETURNING *;

-- name: ReplaceEvent :exec
UPDATE event
SET occurred_at = ?,
  occurred_offset = ?,
  occurred_precision = ?,
  approximate = ?,
  occurred_raw = ?,
  ended_at = ?,
  source_id = ?,
  text = ?,
  url = ?,
  author = ?,
  created_at = ?,
  updated_at = ?
WHERE id = ?;

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = ?;
