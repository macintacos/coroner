package export

import (
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// NewTimeline returns t as its canonical object.
func NewTimeline(t timeline.Timeline) Timeline {
	return Timeline{ID: t.ID, Name: t.Name, Title: t.Title, Refs: t.Refs, CreatedAt: Instant(t.CreatedAt)}
}

// Domain returns t as its domain value, updated when it was created.
func (t Timeline) Domain() timeline.Timeline {
	created := time.Time(t.CreatedAt)

	return timeline.Timeline{ID: t.ID, Name: t.Name, Title: t.Title, Refs: t.Refs, CreatedAt: created, UpdatedAt: created}
}

// NewSource returns s as its canonical object.
func NewSource(s timeline.Source) Source {
	return Source{ID: s.ID, Name: s.Name, CreatedAt: Instant(s.CreatedAt)}
}

// Domain returns s as its domain value.
func (s Source) Domain() timeline.Source {
	return timeline.Source{ID: s.ID, Name: s.Name, CreatedAt: time.Time(s.CreatedAt)}
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

// Domain returns e as its domain value, with a zero seq when e has none yet.
func (e Event) Domain() timeline.Event {
	out := timeline.Event{
		ID:             e.ID,
		TimelineID:     e.TimelineID,
		OccurredAt:     time.Time(e.OccurredAt),
		OccurredOffset: e.OccurredOffset,
		Precision:      e.Precision,
		Approximate:    e.Approximate,
		OccurredRaw:    e.OccurredRaw,
		EndedAt:        (*time.Time)(e.EndedAt),
		Source:         e.Source,
		Tags:           e.Tags,
		Text:           e.Text,
		URL:            e.URL,
		Author:         e.Author,
		CreatedAt:      time.Time(e.CreatedAt),
		UpdatedAt:      time.Time(e.UpdatedAt),
	}
	if e.Seq != nil {
		out.Seq = *e.Seq
	}

	return out
}

// NewAttachment returns a as its canonical object.
func NewAttachment(a timeline.Attachment) Attachment {
	return Attachment{
		ID:           a.ID,
		EventID:      a.Owner.EventID,
		AnnotationID: a.Owner.AnnotationID,
		Filename:     a.Filename,
		MIME:         a.MIME,
		SHA256:       a.SHA256,
		Size:         a.Size,
		CreatedAt:    Instant(a.CreatedAt),
	}
}

// Domain returns a as its domain value.
func (a Attachment) Domain() timeline.Attachment {
	return timeline.Attachment{
		ID:        a.ID,
		Owner:     timeline.AttachmentOwner{EventID: a.EventID, AnnotationID: a.AnnotationID},
		Filename:  a.Filename,
		MIME:      a.MIME,
		SHA256:    a.SHA256,
		Size:      a.Size,
		CreatedAt: time.Time(a.CreatedAt),
	}
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

// Domain returns a as its domain value.
func (a Annotation) Domain() timeline.Annotation {
	return timeline.Annotation{
		ID:         a.ID,
		TimelineID: a.TimelineID,
		EventID:    a.EventID,
		Kind:       a.Kind,
		Body:       a.Body,
		Author:     a.Author,
		Source:     a.Source,
		URL:        a.URL,
		CreatedAt:  time.Time(a.CreatedAt),
		UpdatedAt:  time.Time(a.UpdatedAt),
	}
}
