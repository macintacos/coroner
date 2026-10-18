// Package fixture writes the database the store's compatibility spec reads: timelines,
// events, annotations and sources, written through the store API alone. Every released
// fixture holds these records and the spec compares them, so never change an existing
// record; a release that adds records must also teach the spec which fixtures hold them.
package fixture

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Write creates the coroner database at path and fills it with the fixture's records; it
// errors if path already exists.
func Write(ctx context.Context, path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return fmt.Errorf("%s already exists; write the fixture to a new path", path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	s, err := store.Open(ctx, path)
	if err != nil {
		return err
	}

	return errors.Join(fill(ctx, s), s.Close())
}

func fill(ctx context.Context, s *store.Store) error {
	err := add(Timelines(), func(t timeline.Timeline) error {
		return s.CreateTimeline(ctx, t)
	})
	if err != nil {
		return err
	}

	err = add(Events(), func(e timeline.Event) error {
		_, err := s.AddEvent(ctx, e, sourceID(e.Source))
		return err
	})
	if err != nil {
		return err
	}

	err = add(Annotations(), func(a timeline.Annotation) error {
		return s.AddAnnotation(ctx, a, sourceID(a.Source))
	})
	if err != nil {
		return err
	}

	return s.SetCurrentTimeline(ctx, Timelines()[0].ID)
}

// add validates, then writes, each record in order and stops at the first failure, so the
// fixture holds only what the CLI could write.
func add[T interface{ Validate() error }](records []T, write func(T) error) error {
	for i, record := range records {
		err := record.Validate()
		if err != nil {
			return fmt.Errorf("%T %d: %w", record, i+1, err)
		}
		err = write(record)
		if err != nil {
			return fmt.Errorf("%T %d: %w", record, i+1, err)
		}
	}

	return nil
}

func uuid(n int) string {
	return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
}

// sourceID returns the id Sources gives the source called name, or "" when name is nil
// or Sources has no such source.
func sourceID(name *string) string {
	for _, source := range Sources() {
		if name != nil && source.Name == *name {
			return source.ID
		}
	}

	return ""
}

func createdAt() time.Time {
	return time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
}

// Timelines returns the fixture's timelines as the store reads them back; the first is
// current.
func Timelines() []timeline.Timeline {
	return []timeline.Timeline{
		{
			ID:        uuid(1),
			Name:      "inc-2041",
			Title:     "Checkout latency spike",
			Refs:      []string{"INC-2041", "https://status.example.com/incidents/2041", "PM-77"},
			CreatedAt: createdAt(),
			UpdatedAt: createdAt(),
		},
		{ID: uuid(2), Name: "inc-2042", Refs: []string{}, CreatedAt: createdAt(), UpdatedAt: createdAt()},
	}
}

// Events returns the fixture's events as the store reads them back: one per precision in
// inc-2041, covering approximate, interval, offset, source and tag variants, and one in
// inc-2042.
func Events() []timeline.Event {
	const pdt, bst, ist = -7 * 3600, 3600, 5*3600 + 1800

	return []timeline.Event{
		{
			ID: uuid(11), TimelineID: uuid(1), Seq: 1,
			OccurredAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Precision: timeline.PrecisionDay,
			Approximate: true, OccurredRaw: new("sometime on the 12th"), Tags: []string{},
			Text:      "Cache eviction rate started climbing",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(12), TimelineID: uuid(1), Seq: 2,
			OccurredAt:     time.Date(2026, 9, 12, 9, 0, 0, 0, time.FixedZone("", pdt)).UTC(),
			OccurredOffset: new(pdt), Precision: timeline.PrecisionHour, OccurredRaw: new("9am PDT"),
			Source: new("pagerduty"), Tags: []string{"alert", "sev1"}, Author: new("sam"),
			Text:      "Checkout p99 latency alert fired",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(13), TimelineID: uuid(1), Seq: 3,
			OccurredAt:     time.Date(2026, 9, 12, 17, 15, 0, 0, time.FixedZone("", bst)).UTC(),
			OccurredOffset: new(bst), Precision: timeline.PrecisionMinute,
			EndedAt: new(time.Date(2026, 9, 12, 16, 45, 0, 0, time.UTC)),
			Source:  new("grafana"), Tags: []string{"latency", "phase:detect"},
			URL:       new("https://grafana.example.com/d/checkout"),
			Text:      "Checkout latency above its SLO",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(14), TimelineID: uuid(1), Seq: 4,
			OccurredAt: time.Date(2026, 9, 12, 16, 20, 7, 0, time.UTC), Precision: timeline.PrecisionSecond,
			Source: new("slack"), Tags: []string{"phase:respond"},
			Text:      "Incident channel opened",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(15), TimelineID: uuid(1), Seq: 5,
			OccurredAt:     time.Date(2026, 9, 12, 21, 52, 31, 250_000_000, time.FixedZone("", ist)).UTC(),
			OccurredOffset: new(ist), Precision: timeline.PrecisionMillisecond,
			Tags:      []string{"deploy", "rollback"},
			Text:      "Cache config rolled back",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(16), TimelineID: uuid(1), Seq: 6,
			OccurredAt: time.Date(2026, 9, 12, 16, 23, 0, 125_000, time.UTC), OccurredOffset: new(0),
			Precision: timeline.PrecisionMicrosecond, Source: new("pagerduty"), Tags: []string{},
			Text:      "Alert acknowledged",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(17), TimelineID: uuid(1), Seq: 7,
			OccurredAt: time.Date(2026, 9, 12, 16, 24, 59, 123_456_789, time.UTC), Precision: timeline.PrecisionNanosecond,
			Source: new("slack"), Tags: []string{},
			Text:      "Checkout latency back under its SLO",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(21), TimelineID: uuid(2), Seq: 1,
			OccurredAt: time.Date(2026, 9, 12, 19, 5, 0, 0, time.UTC), Precision: timeline.PrecisionMinute,
			Tags:      []string{},
			Text:      "Disk usage alert on db-2",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
	}
}

// Annotations returns the fixture's annotations as the store reads them back.
func Annotations() []timeline.Annotation {
	return []timeline.Annotation{
		{
			ID: uuid(31), TimelineID: uuid(1), EventID: new(uuid(12)), Kind: timeline.AnnotationEvidence,
			Body:   "PagerDuty: checkout p99 above 2s for 5 minutes",
			Source: new("pagerduty"), URL: new("https://example.pagerduty.com/incidents/P2041"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(32), TimelineID: uuid(1), EventID: new(uuid(13)), Kind: timeline.AnnotationComment,
			Body: "Latency tracks the eviction rate", Author: new("sam"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: uuid(33), TimelineID: uuid(1), Kind: timeline.AnnotationComment,
			Body: "Root cause: the cache was sized for last year's traffic", Author: new("sam"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
	}
}

// Sources returns the sources the fixture's events and annotations name, as the store reads
// them back.
func Sources() []timeline.Source {
	return []timeline.Source{
		{ID: uuid(42), Name: "grafana", CreatedAt: createdAt()},
		{ID: uuid(41), Name: "pagerduty", CreatedAt: createdAt()},
		{ID: uuid(43), Name: "slack", CreatedAt: createdAt()},
	}
}
