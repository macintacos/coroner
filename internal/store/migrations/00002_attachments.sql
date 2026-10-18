-- +goose Up
CREATE TABLE attachment (
  id TEXT PRIMARY KEY CHECK (length(id) = 36),
  event_id TEXT REFERENCES event (id) ON DELETE CASCADE,
  annotation_id TEXT REFERENCES annotation (id) ON DELETE CASCADE,
  filename TEXT NOT NULL, -- display only; blobs are keyed by sha256
  mime TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size INTEGER NOT NULL CHECK (size >= 0),
  created_at TEXT NOT NULL CHECK (created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK ((event_id IS NULL) <> (annotation_id IS NULL))
) STRICT;

CREATE INDEX attachment_by_event ON attachment (event_id);

CREATE INDEX attachment_by_annotation ON attachment (annotation_id);

CREATE INDEX attachment_by_sha256 ON attachment (sha256);
