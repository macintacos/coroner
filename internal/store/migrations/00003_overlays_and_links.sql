-- +goose Up
CREATE TABLE overlay (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  name TEXT NOT NULL UNIQUE CHECK (name GLOB '[a-z0-9]*' AND name NOT GLOB '*[^a-z0-9._-]*' AND length(name) <= 64),
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE overlay_timeline (
  overlay_id TEXT NOT NULL REFERENCES overlay (id) ON DELETE CASCADE,
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  position INTEGER NOT NULL, -- the order the timelines were given in
  PRIMARY KEY (overlay_id, timeline_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX overlay_timeline_by_timeline ON overlay_timeline (timeline_id);

CREATE TABLE event_link (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  from_event_id TEXT NOT NULL REFERENCES event (id) ON DELETE CASCADE,
  to_event_id TEXT NOT NULL REFERENCES event (id) ON DELETE CASCADE,
  -- Adding a kind rebuilds this table; nothing references it, so that needs no foreign_keys = OFF.
  kind TEXT NOT NULL CHECK (kind IN ('led_to', 'same_as', 'copied_from')),
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  author TEXT,
  CHECK (from_event_id <> to_event_id),
  UNIQUE (from_event_id, to_event_id, kind)
) STRICT;

CREATE INDEX event_link_by_to_event ON event_link (to_event_id);
