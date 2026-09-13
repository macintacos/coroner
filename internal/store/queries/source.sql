-- name: CreateSource :one
INSERT INTO source (
  id, name, created_at
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: UpsertSource :one
-- UpsertSource returns the existing source when name is taken, ignoring id and created_at; the no-op DO UPDATE is what makes RETURNING yield that row.
INSERT INTO source (
  id, name, created_at
) VALUES (
  ?, ?, ?
)
ON CONFLICT (name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: GetSource :one
SELECT * FROM source
WHERE id = ?;

-- name: GetSourceByName :one
SELECT * FROM source
WHERE name = ?;

-- name: ListSources :many
SELECT * FROM source
ORDER BY name;

-- name: UpdateSource :one
UPDATE source
SET name = ?
WHERE id = ?
RETURNING *;

-- name: DeleteSource :exec
DELETE FROM source
WHERE id = ?;
