-- name: GetCurrentTimeline :one
SELECT value FROM setting
WHERE key = 'current_timeline';

-- name: SetCurrentTimeline :exec
INSERT INTO setting (
  key, value
) VALUES (
  'current_timeline', ?
)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: ClearCurrentTimeline :exec
DELETE FROM setting
WHERE key = 'current_timeline';
