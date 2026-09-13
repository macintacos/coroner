// Package fixture writes the database the store's compatibility spec reads: every kind of
// record the store keeps, written through the store API alone.
package fixture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Write creates the coroner database at path, which must not exist yet, and fills it with
// the fixture's records.
func Write(ctx context.Context, path string) error {
	s, err := store.Open(ctx, path)
	if err != nil {
		return err
	}

	return errors.Join(fill(ctx, s), s.Close())
}

func fill(ctx context.Context, s *store.Store) error {
	err := add(timelines(), func(t timeline.Timeline) error {
		return s.CreateTimeline(ctx, t)
	})
	if err != nil {
		return err
	}

	err = add(events(), func(e timeline.Event) error {
		_, err := s.AddEvent(ctx, e, newSourceID(e.Source))
		return err
	})
	if err != nil {
		return err
	}

	err = add(annotations(), func(a timeline.Annotation) error {
		return s.AddAnnotation(ctx, a, newSourceID(a.Source))
	})
	if err != nil {
		return err
	}

	return s.SetCurrentTimeline(ctx, id(1))
}

// add writes each record that passes its domain validation, so the fixture holds only what
// the CLI could write, and stops at the first failure.
func add[T interface{ Validate() error }](records []T, write func(T) error) error {
	for i, record := range records {
		err := record.Validate()
		if err == nil {
			err = write(record)
		}
		if err != nil {
			return fmt.Errorf("%T %d: %w", record, i+1, err)
		}
	}

	return nil
}

func id(n int) string {
	return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
}

func newSourceID(name *string) string {
	if name == nil {
		return ""
	}

	return map[string]string{"pagerduty": id(41), "grafana": id(42), "slack": id(43)}[*name]
}

func createdAt() time.Time {
	return time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
}

func timelines() []timeline.Timeline {
	return []timeline.Timeline{
		{
			ID:        id(1),
			Name:      "inc-2041",
			Title:     "Checkout latency spike",
			Refs:      []string{"INC-2041", "https://status.example.com/incidents/2041", "PM-77"},
			CreatedAt: createdAt(),
			UpdatedAt: createdAt(),
		},
		{ID: id(2), Name: "inc-2042", CreatedAt: createdAt(), UpdatedAt: createdAt()},
	}
}

// events holds one event per precision in inc-2041, covering approximate, interval, offset,
// source and tag variants, and one event in inc-2042.
func events() []timeline.Event {
	const pdt, bst, ist = -7 * 3600, 3600, 5*3600 + 1800

	return []timeline.Event{
		{
			ID: id(11), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Precision: timeline.PrecisionDay,
			Approximate: true, OccurredRaw: new("sometime on the 12th"),
			Text:      "Cache eviction rate started climbing",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(12), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 9, 0, 0, 0, time.FixedZone("", pdt)), OccurredOffset: new(pdt),
			Precision: timeline.PrecisionHour, OccurredRaw: new("9am PDT"),
			Source: new("pagerduty"), Tags: []string{"alert", "sev1"}, Author: new("sam"),
			Text:      "Checkout p99 latency alert fired",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(13), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 17, 15, 0, 0, time.FixedZone("", bst)), OccurredOffset: new(bst),
			Precision: timeline.PrecisionMinute, EndedAt: new(time.Date(2026, 9, 12, 16, 45, 0, 0, time.UTC)),
			Source: new("grafana"), Tags: []string{"latency", "phase:detect"},
			URL:       new("https://grafana.example.com/d/checkout"),
			Text:      "Checkout latency above its SLO",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(14), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 16, 20, 7, 0, time.UTC), Precision: timeline.PrecisionSecond,
			Source: new("slack"), Tags: []string{"phase:respond"},
			Text:      "Incident channel opened",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(15), TimelineID: id(1),
			OccurredAt:     time.Date(2026, 9, 12, 21, 52, 31, 250_000_000, time.FixedZone("", ist)),
			OccurredOffset: new(ist), Precision: timeline.PrecisionMillisecond,
			Tags:      []string{"deploy", "rollback"},
			Text:      "Cache config rolled back",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(16), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 16, 23, 0, 125_000, time.UTC), OccurredOffset: new(0),
			Precision: timeline.PrecisionMicrosecond, Source: new("pagerduty"),
			Text:      "Alert acknowledged",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(17), TimelineID: id(1),
			OccurredAt: time.Date(2026, 9, 12, 16, 24, 59, 123_456_789, time.UTC), Precision: timeline.PrecisionNanosecond,
			Source:    new("slack"),
			Text:      "Checkout latency back under its SLO",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(21), TimelineID: id(2),
			OccurredAt: time.Date(2026, 9, 12, 19, 5, 0, 0, time.UTC), Precision: timeline.PrecisionMinute,
			Text:      "Disk usage alert on db-2",
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
	}
}

func annotations() []timeline.Annotation {
	return []timeline.Annotation{
		{
			ID: id(31), TimelineID: id(1), EventID: new(id(12)), Kind: timeline.AnnotationEvidence,
			Body:   "PagerDuty: checkout p99 above 2s for 5 minutes",
			Source: new("pagerduty"), URL: new("https://example.pagerduty.com/incidents/P2041"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(32), TimelineID: id(1), EventID: new(id(13)), Kind: timeline.AnnotationComment,
			Body: "Latency tracks the eviction rate", Author: new("sam"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
		{
			ID: id(33), TimelineID: id(1), Kind: timeline.AnnotationComment,
			Body: "Root cause: the cache was sized for last year's traffic", Author: new("sam"),
			CreatedAt: createdAt(), UpdatedAt: createdAt(),
		},
	}
}
