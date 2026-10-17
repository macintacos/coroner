-- +goose Up
PRAGMA application_id = 1668248174; -- 0x636f726e 'corn'

CREATE TABLE timeline (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  name TEXT NOT NULL UNIQUE CHECK (name GLOB '[a-z0-9]*' AND name NOT GLOB '*[^a-z0-9._-]*' AND length(name) <= 64),
  title TEXT NOT NULL DEFAULT '',
  next_seq INTEGER NOT NULL DEFAULT 1, -- only ever increases, so a deleted event's seq is never reused
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE timeline_ref (
  position INTEGER PRIMARY KEY, -- an explicit rowid alias survives VACUUM, so refs keep the order they were added
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  ref TEXT NOT NULL CHECK (length(trim(ref, char(9, 10, 11, 12, 13, 32, 133, 160, 5760, 8192, 8193, 8194, 8195, 8196, 8197, 8198, 8199, 8200, 8201, 8202, 8232, 8233, 8239, 8287, 12288))) > 0),
  UNIQUE (timeline_id, ref)
) STRICT;

CREATE TABLE source (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  name TEXT NOT NULL UNIQUE CHECK (name GLOB '[a-z0-9]*' AND name NOT GLOB '*[^a-z0-9._:/-]*' AND length(name) <= 64),
  timezone TEXT, -- reserved; nothing reads or writes it yet
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE event (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  seq INTEGER NOT NULL CHECK (seq > 0),
  occurred_at TEXT NOT NULL CHECK (occurred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  occurred_offset INTEGER CHECK (occurred_offset BETWEEN -86400 AND 86400), -- seconds east of UTC
  occurred_precision TEXT NOT NULL CHECK (occurred_precision IN ('day', 'hour', 'minute', 'second', 'millisecond', 'microsecond', 'nanosecond')),
  approximate INTEGER NOT NULL DEFAULT 0 CHECK (approximate IN (0, 1)),
  occurred_raw TEXT,
  ended_at TEXT CHECK (ended_at IS NULL OR (ended_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND ended_at >= occurred_at)),
  source_id TEXT REFERENCES source (id) ON DELETE RESTRICT,
  text TEXT NOT NULL CHECK (length(trim(text, char(9, 10, 11, 12, 13, 32, 133, 160, 5760, 8192, 8193, 8194, 8195, 8196, 8197, 8198, 8199, 8200, 8201, 8202, 8232, 8233, 8239, 8287, 12288))) > 0),
  url TEXT,
  author TEXT,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  UNIQUE (timeline_id, seq),
  UNIQUE (id, timeline_id) -- parent key for annotation's (event_id, timeline_id) foreign key
) STRICT;

CREATE INDEX event_by_time ON event (timeline_id, occurred_at, seq);

CREATE INDEX event_by_source ON event (source_id);

CREATE TABLE event_tag (
  event_id TEXT NOT NULL REFERENCES event (id) ON DELETE CASCADE,
  tag TEXT NOT NULL CHECK (tag GLOB '[a-z0-9]*' AND tag NOT GLOB '*[^a-z0-9._:/-]*' AND length(tag) <= 64),
  PRIMARY KEY (event_id, tag)
) STRICT, WITHOUT ROWID;

CREATE INDEX event_tag_by_tag ON event_tag (tag);

CREATE TABLE annotation (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  event_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('evidence', 'comment')),
  body TEXT NOT NULL CHECK (length(trim(body, char(9, 10, 11, 12, 13, 32, 133, 160, 5760, 8192, 8193, 8194, 8195, 8196, 8197, 8198, 8199, 8200, 8201, 8202, 8232, 8233, 8239, 8287, 12288))) > 0),
  author TEXT,
  source_id TEXT REFERENCES source (id) ON DELETE RESTRICT,
  url TEXT,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  -- not checked when event_id is NULL: an annotation on the whole timeline
  FOREIGN KEY (event_id, timeline_id) REFERENCES event (id, timeline_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX annotation_by_event ON annotation (event_id);

CREATE TABLE setting (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

-- +goose StatementBegin
CREATE TRIGGER timeline_delete_clears_current AFTER DELETE ON timeline BEGIN
  DELETE FROM setting WHERE key = 'current_timeline' AND value = OLD.id;
END;
-- +goose StatementEnd
