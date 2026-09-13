-- +goose Up
PRAGMA application_id = 1668248174; -- 0x636f726e 'corn'

CREATE TABLE timeline (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  name TEXT NOT NULL UNIQUE CHECK (name GLOB '[a-z0-9]*' AND name NOT GLOB '*[^a-z0-9._-]*' AND length(name) <= 64),
  title TEXT NOT NULL DEFAULT '',
  next_seq INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE timeline_ref (
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  ref TEXT NOT NULL CHECK (length(trim(ref)) > 0),
  PRIMARY KEY (timeline_id, ref)
) STRICT;

CREATE TABLE source (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  name TEXT NOT NULL UNIQUE CHECK (name GLOB '[a-z0-9]*' AND name NOT GLOB '*[^a-z0-9._:/-]*' AND length(name) <= 64),
  timezone TEXT,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE event (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  timeline_id TEXT NOT NULL REFERENCES timeline (id) ON DELETE CASCADE,
  seq INTEGER NOT NULL CHECK (seq > 0),
  occurred_at TEXT NOT NULL CHECK (occurred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  occurred_offset INTEGER CHECK (occurred_offset BETWEEN -86400 AND 86400),
  occurred_precision TEXT NOT NULL CHECK (occurred_precision IN ('day', 'hour', 'minute', 'second', 'millisecond', 'microsecond', 'nanosecond')),
  approximate INTEGER NOT NULL DEFAULT 0 CHECK (approximate IN (0, 1)),
  occurred_raw TEXT,
  ended_at TEXT CHECK (ended_at IS NULL OR (ended_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND ended_at >= occurred_at)),
  source_id TEXT REFERENCES source (id) ON DELETE RESTRICT,
  text TEXT NOT NULL CHECK (length(trim(text)) > 0),
  url TEXT,
  author TEXT,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  UNIQUE (timeline_id, seq),
  UNIQUE (id, timeline_id)
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
  body TEXT NOT NULL CHECK (length(trim(body)) > 0),
  author TEXT,
  source_id TEXT REFERENCES source (id) ON DELETE RESTRICT,
  url TEXT,
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  updated_at TEXT NOT NULL CHECK (updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
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
