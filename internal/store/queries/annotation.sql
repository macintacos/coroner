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

-- name: ListAnnotationsByIDPrefix :many
SELECT * FROM annotation
WHERE id GLOB ?
ORDER BY id
LIMIT ?;

-- name: ListAnnotations :many
SELECT * FROM annotation
WHERE timeline_id = ?
ORDER BY created_at, id;

-- name: ListEventAnnotations :many
SELECT * FROM annotation
WHERE event_id = ?
ORDER BY created_at, id;

-- name: OverwriteAnnotation :one
UPDATE annotation
SET kind = sqlc.arg(kind),
  body = sqlc.arg(body),
  author = sqlc.arg(author),
  source_id = sqlc.arg(source_id),
  url = sqlc.arg(url),
  created_at = COALESCE(sqlc.narg(created_at), created_at),
  updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING id;

-- name: DeleteAnnotation :exec
DELETE FROM annotation
WHERE id = ?;
