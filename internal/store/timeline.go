package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// CreateTimeline adds t with its refs, kept in the order given.
func (s *Store) CreateTimeline(ctx context.Context, t timeline.Timeline) error {
	err := t.Validate()
	if err != nil {
		return err
	}

	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return createTimeline(ctx, q, t)
	})
}

// Timeline returns the timeline with id.
func (s *Store) Timeline(ctx context.Context, id string) (timeline.Timeline, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Timeline, error) {
		row, err := q.GetTimeline(ctx, id)
		if err != nil {
			return timeline.Timeline{}, err
		}

		return loadTimeline(ctx, q, row)
	})
}

// TimelineByName returns the timeline called name.
func (s *Store) TimelineByName(ctx context.Context, name string) (timeline.Timeline, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Timeline, error) {
		row, err := q.GetTimelineByName(ctx, name)
		if err != nil {
			return timeline.Timeline{}, err
		}

		return loadTimeline(ctx, q, row)
	})
}

// CurrentTimeline returns the timeline commands act on when none is named, or ErrNotFound
// when none is set.
func (s *Store) CurrentTimeline(ctx context.Context) (timeline.Timeline, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Timeline, error) {
		id, err := q.GetCurrentTimeline(ctx)
		if err != nil {
			return timeline.Timeline{}, err
		}
		row, err := q.GetTimeline(ctx, id)
		if err != nil {
			return timeline.Timeline{}, err
		}

		return loadTimeline(ctx, q, row)
	})
}

// Timelines returns every timeline, ordered by name.
func (s *Store) Timelines(ctx context.Context) ([]timeline.Timeline, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Timeline, error) {
		rows, err := q.ListTimelines(ctx)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Timeline) (timeline.Timeline, error) { return loadTimeline(ctx, q, r) })
	})
}

// UpdateTimeline stores t's name, title and updated at, and replaces its refs with t.Refs
// in the order given.
func (s *Store) UpdateTimeline(ctx context.Context, t timeline.Timeline) error {
	err := t.Validate()
	if err != nil {
		return err
	}

	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.UpdateTimeline(ctx, sqlcgen.UpdateTimelineParams{
			Name:      t.Name,
			Title:     t.Title,
			UpdatedAt: timeline.FormatInstant(t.UpdatedAt),
			ID:        t.ID,
		})
		if err != nil {
			return err
		}

		return replaceRefs(ctx, q, t.ID, t.Refs)
	})
}

// DeleteTimeline deletes the timeline with id and everything in it.
func (s *Store) DeleteTimeline(ctx context.Context, id string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetTimeline(ctx, id)
		if err != nil {
			return err
		}

		return q.DeleteTimeline(ctx, id)
	})
}

// SetCurrentTimeline makes the timeline with id the current timeline.
func (s *Store) SetCurrentTimeline(ctx context.Context, id string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetTimeline(ctx, id)
		if err != nil {
			return err
		}

		return q.SetCurrentTimeline(ctx, id)
	})
}

// ClearCurrentTimeline leaves no timeline current.
func (s *Store) ClearCurrentTimeline(ctx context.Context) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		return q.ClearCurrentTimeline(ctx)
	})
}

func loadTimeline(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Timeline) (timeline.Timeline, error) {
	refs, err := q.ListTimelineRefs(ctx, row.ID)
	if err != nil {
		return timeline.Timeline{}, err
	}

	var p instants
	t := timeline.Timeline{
		ID:        row.ID,
		Name:      row.Name,
		Title:     row.Title,
		Refs:      refs,
		CreatedAt: p.parse(row.CreatedAt),
		UpdatedAt: p.parse(row.UpdatedAt),
	}

	return t, p.err
}

func createTimeline(ctx context.Context, q *sqlcgen.Queries, t timeline.Timeline) error {
	_, err := q.CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
		ID:        t.ID,
		Name:      t.Name,
		Title:     t.Title,
		CreatedAt: timeline.FormatInstant(t.CreatedAt),
		UpdatedAt: timeline.FormatInstant(t.UpdatedAt),
	})
	if err != nil {
		return err
	}

	return addRefs(ctx, q, t.ID, t.Refs)
}

func addRefs(ctx context.Context, q *sqlcgen.Queries, timelineID string, refs []string) error {
	for _, ref := range refs {
		err := q.AddTimelineRef(ctx, sqlcgen.AddTimelineRefParams{TimelineID: timelineID, Ref: ref})
		if err != nil {
			return err
		}
	}

	return nil
}

// replaceRefs replaces the refs of the timeline with timelineID with refs, in the order given.
func replaceRefs(ctx context.Context, q *sqlcgen.Queries, timelineID string, refs []string) error {
	stored, err := q.ListTimelineRefs(ctx, timelineID)
	if err != nil {
		return err
	}
	for _, ref := range stored {
		err = q.DeleteTimelineRef(ctx, sqlcgen.DeleteTimelineRefParams{TimelineID: timelineID, Ref: ref})
		if err != nil {
			return err
		}
	}

	return addRefs(ctx, q, timelineID, refs)
}
