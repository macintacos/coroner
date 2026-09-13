// Package export is coroner's public machine representation: the canonical JSON object for
// each timeline, source, event and annotation, the versioned export envelope that carries
// them, and the one encoder that writes them. Every key is always present, so no field here
// uses omitempty or omitzero; a key that may be missing is a pointer, which encodes as null.
package export

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// Format identifies a coroner export; FormatVersion is the version of the format it follows.
const (
	Format        = "coroner.export"
	FormatVersion = "1.0"
)

// Envelope is a whole export: what was exported, by which coroner, when and with which
// query, and the flat record lists.
type Envelope struct {
	// Format is always "coroner.export".
	Format string `json:"format"`
	// FormatVersion is the MAJOR.MINOR version of the export format.
	FormatVersion string `json:"format_version"`
	// CoronerVersion is the version of coroner that wrote the export.
	CoronerVersion string `json:"coroner_version"`
	// ExportedAt is when the export was written.
	ExportedAt Instant `json:"exported_at"`
	// Query is the selection the export was made with.
	Query       Query        `json:"query"`
	Timelines   []Timeline   `json:"timelines"`
	Sources     []Source     `json:"sources"`
	Events      []Event      `json:"events"`
	Annotations []Annotation `json:"annotations"`
}

// Query is the selection an export was made with. Every field but Timelines filters events,
// and null or an empty list means that filter was not applied.
type Query struct {
	// Timelines are the names of the timelines exported.
	Timelines []string `json:"timelines"`
	// Since keeps events that occurred at or after it.
	Since *Instant `json:"since" jsonschema:"nullable"`
	// Until keeps events that occurred before it.
	Until *Instant `json:"until" jsonschema:"nullable"`
	// Sources keeps events from any of these source names.
	Sources []string `json:"sources"`
	// Tags keeps events carrying all of these tags.
	Tags []string `json:"tags"`
	// Contains keeps events whose text contains it, ignoring ASCII case.
	Contains *string `json:"contains" jsonschema:"nullable"`
	// Limit caps how many events were exported.
	Limit *int `json:"limit" jsonschema:"nullable"`
}

// Timeline is the named chronology of one investigation.
type Timeline struct {
	ID string `json:"id"`
	// Name is the timeline's unique name, such as mongo-outage.
	Name string `json:"name"`
	// Title is a free-text heading; it may be empty.
	Title string `json:"title"`
	// Refs are issue-tracker keys or URLs the timeline belongs to, kept as written.
	Refs      []string `json:"refs"`
	CreatedAt Instant  `json:"created_at"`
}

// Source is the system, service or person an event or annotation came from.
type Source struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	CreatedAt Instant `json:"created_at"`
}

// Event is something that happened at a moment, or over an interval, as a source reported it.
type Event struct {
	ID         string `json:"id"`
	TimelineID string `json:"timeline_id"`
	// Seq is the event's permanent per-timeline counter value; null only in a preview of an
	// event not yet added.
	Seq *int64 `json:"seq" jsonschema:"nullable"`
	// OccurredAt is when the event happened, or its interval started: the start of its
	// precision window.
	OccurredAt Instant `json:"occurred_at"`
	// OccurredOffset is the input's offset from UTC in seconds, positive east; null when the
	// input was Z or an epoch, which say nothing about a local offset.
	OccurredOffset *int `json:"occurred_offset" jsonschema:"nullable"`
	// Precision is how exact occurred_at is, from day to nanosecond. Consumers must accept
	// values they do not know.
	Precision timeline.Precision `json:"precision"`
	// Approximate marks occurred_at as a guess.
	Approximate bool `json:"approximate"`
	// OccurredRaw is the time exactly as it was given.
	OccurredRaw *string `json:"occurred_raw" jsonschema:"nullable"`
	// EndedAt is when the interval ended; null for an event at a single moment.
	EndedAt *Instant `json:"ended_at" jsonschema:"nullable"`
	// Source is the name of the source the event came from.
	Source *string  `json:"source" jsonschema:"nullable"`
	Tags   []string `json:"tags"`
	Text   string   `json:"text"`
	// URL is a permalink to the thing itself, such as the log line or chat message.
	URL *string `json:"url" jsonschema:"nullable"`
	// Author is the person who added the event, not necessarily its source.
	Author    *string `json:"author"     jsonschema:"nullable"`
	CreatedAt Instant `json:"created_at"`
	UpdatedAt Instant `json:"updated_at"`
}

// Annotation is evidence or commentary attached to an event, or to its whole timeline.
type Annotation struct {
	ID         string `json:"id"`
	TimelineID string `json:"timeline_id"`
	// EventID is the event annotated; null for an annotation on the whole timeline.
	EventID *string `json:"event_id" jsonschema:"nullable"`
	// Kind is evidence or comment. Consumers must accept values they do not know.
	Kind timeline.AnnotationKind `json:"kind"`
	Body string                  `json:"body"`
	// Author is the person who added the annotation, not necessarily its source.
	Author *string `json:"author" jsonschema:"nullable"`
	// Source is the name of the source the annotation came from.
	Source *string `json:"source" jsonschema:"nullable"`
	// URL is a permalink to the thing itself, such as the log line or chat message.
	URL       *string `json:"url"        jsonschema:"nullable"`
	CreatedAt Instant `json:"created_at"`
	UpdatedAt Instant `json:"updated_at"`
}

// NewEnvelope returns an envelope with the format identifiers set, written by coronerVersion
// at exportedAt, with an empty query and no records.
func NewEnvelope(coronerVersion string, exportedAt time.Time) Envelope {
	return Envelope{
		Format:         Format,
		FormatVersion:  FormatVersion,
		CoronerVersion: coronerVersion,
		ExportedAt:     Instant(exportedAt),
	}
}

// Write encodes v and a newline to w: struct keys in declaration order, map keys sorted, and
// <, > and & escaped so the JSON can be embedded in HTML. It fails on invalid UTF-8.
func Write(w io.Writer, v any) error {
	err := json.MarshalWrite(w, v, json.Deterministic(true), jsontext.EscapeForHTML(true))
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, "\n")
	return err
}
