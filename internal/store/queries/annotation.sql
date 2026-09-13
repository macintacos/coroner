-- name: CreateAnnotation :one
INSERT INTO annotation (
  id, timeline_id, event_id, kind, body, author, source_id, url, created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetAnnotation :one
SELECT * FROM annotation
WHERE id = ?;

-- name: ListAnnotationsByIDPattern :many
SELECT * FROM annotation
WHERE id GLOB sqlc.arg(pattern)
ORDER BY id
LIMIT sqlc.arg(max_rows);

-- name: ListAnnotations :many
SELECT * FROM annotation
WHERE timeline_id = ?
ORDER BY created_at, id;

-- name: ListEventAnnotations :many
SELECT * FROM annotation
WHERE event_id = ?
ORDER BY created_at, id;

-- name: UpdateAnnotation :one
UPDATE annotation
SET kind = ?,
  body = ?,
  author = ?,
  source_id = ?,
  url = ?,
  updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteAnnotation :exec
DELETE FROM annotation
WHERE id = ?;
