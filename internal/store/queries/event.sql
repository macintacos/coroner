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

-- name: OverwriteEvent :one
UPDATE event
SET occurred_at = sqlc.arg(occurred_at),
  occurred_offset = sqlc.arg(occurred_offset),
  occurred_precision = sqlc.arg(occurred_precision),
  approximate = sqlc.arg(approximate),
  occurred_raw = sqlc.arg(occurred_raw),
  ended_at = sqlc.arg(ended_at),
  source_id = sqlc.arg(source_id),
  text = sqlc.arg(text),
  url = sqlc.arg(url),
  author = sqlc.arg(author),
  created_at = COALESCE(sqlc.narg(created_at), created_at),
  updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING id;

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = ?;
