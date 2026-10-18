-- name: CreateAttachment :one
INSERT INTO attachment (
  id, event_id, annotation_id, filename, mime, sha256, size, created_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = ?;

-- name: ListEventAttachments :many
SELECT * FROM attachment
WHERE event_id = ?
ORDER BY created_at, id;

-- name: ListAnnotationAttachments :many
SELECT * FROM attachment
WHERE annotation_id = ?
ORDER BY created_at, id;

-- name: ListTimelineAttachments :many
SELECT * FROM attachment
WHERE event_id IN (SELECT event.id FROM event WHERE event.timeline_id = sqlc.arg(timeline_id))
  OR annotation_id IN (SELECT annotation.id FROM annotation WHERE annotation.timeline_id = sqlc.arg(timeline_id))
ORDER BY created_at, id;

-- name: ListAttachmentsByIDPrefix :many
SELECT * FROM attachment
WHERE id GLOB ?
ORDER BY id
LIMIT ?;

-- name: ListAttachmentHashes :many
SELECT DISTINCT sha256 FROM attachment;

-- name: DeleteAttachment :exec
DELETE FROM attachment
WHERE id = ?;
