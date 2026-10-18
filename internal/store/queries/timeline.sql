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

-- name: ReplaceTimeline :exec
UPDATE timeline
SET name = ?,
  title = ?,
  created_at = ?,
  updated_at = ?
WHERE id = ?;

-- name: SetNextSeq :exec
UPDATE timeline
SET next_seq = ?
WHERE id = ?;

-- name: DeleteTimeline :exec
DELETE FROM timeline
WHERE id = ?;

-- name: AllocateSeq :one
-- AllocateSeq returns the timeline's next seq and advances its counter; call it in the transaction that creates the event.
UPDATE timeline
SET next_seq = next_seq + 1
WHERE id = ?
RETURNING next_seq - 1;
