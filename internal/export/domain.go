package export

import "github.com/macintacos/coroner/internal/timeline"

// NewTimeline returns t as its canonical object.
func NewTimeline(t timeline.Timeline) Timeline {
	return Timeline{ID: t.ID, Name: t.Name, Title: t.Title, Refs: t.Refs, CreatedAt: Instant(t.CreatedAt)}
}

// NewSource returns s as its canonical object.
func NewSource(s timeline.Source) Source {
	return Source{ID: s.ID, Name: s.Name, CreatedAt: Instant(s.CreatedAt)}
}

// NewEvent returns e as its canonical object, with a null seq when e has none yet.
func NewEvent(e timeline.Event) Event {
	out := Event{
		ID:             e.ID,
		TimelineID:     e.TimelineID,
		OccurredAt:     Instant(e.OccurredAt),
		OccurredOffset: e.OccurredOffset,
		Precision:      e.Precision,
		Approximate:    e.Approximate,
		OccurredRaw:    e.OccurredRaw,
		EndedAt:        (*Instant)(e.EndedAt),
		Source:         e.Source,
		Tags:           e.Tags,
		Text:           e.Text,
		URL:            e.URL,
		Author:         e.Author,
		CreatedAt:      Instant(e.CreatedAt),
		UpdatedAt:      Instant(e.UpdatedAt),
	}
	if e.Seq > 0 {
		out.Seq = &e.Seq
	}

	return out
}

// NewAnnotation returns a as its canonical object.
func NewAnnotation(a timeline.Annotation) Annotation {
	return Annotation{
		ID:         a.ID,
		TimelineID: a.TimelineID,
		EventID:    a.EventID,
		Kind:       a.Kind,
		Body:       a.Body,
		Author:     a.Author,
		Source:     a.Source,
		URL:        a.URL,
		CreatedAt:  Instant(a.CreatedAt),
		UpdatedAt:  Instant(a.UpdatedAt),
	}
}
