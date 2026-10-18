-- name: CreateOverlay :one
INSERT INTO overlay (
  id, name, created_at
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: AddOverlayTimeline :exec
INSERT INTO overlay_timeline (
  overlay_id, timeline_id, position
) VALUES (
  ?, ?, ?
);

-- name: GetOverlayByName :one
SELECT * FROM overlay
WHERE name = ?;

-- name: ListOverlayTimelineIDs :many
SELECT timeline_id FROM overlay_timeline
WHERE overlay_id = ?
ORDER BY position;

-- name: ListTimelineOverlays :many
SELECT * FROM overlay
WHERE id IN (SELECT overlay_timeline.overlay_id FROM overlay_timeline WHERE overlay_timeline.timeline_id = ?)
ORDER BY name;
