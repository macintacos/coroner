-- name: CreateTimeline :one
INSERT INTO timeline (
  id, name, title, created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetTimeline :one
SELECT * FROM timeline
WHERE id = ?;

-- name: GetTimelineByName :one
SELECT * FROM timeline
WHERE name = ?;

-- name: ListTimelines :many
SELECT * FROM timeline
ORDER BY name;

-- name: UpdateTimeline :one
UPDATE timeline
SET name = ?,
  title = ?,
  updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteTimeline :exec
DELETE FROM timeline
WHERE id = ?;

-- name: AllocateSeq :one
UPDATE timeline
SET next_seq = next_seq + 1
WHERE id = ?
RETURNING next_seq - 1;
