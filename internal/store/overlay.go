package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// CreateOverlay saves o with its timelines, kept in the order given. It returns an error
// wrapping ErrConflict when another overlay has o's name.
func (s *Store) CreateOverlay(ctx context.Context, o timeline.Overlay) error {
	err := o.Validate()
	if err != nil {
		return err
	}
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.CreateOverlay(ctx, sqlcgen.CreateOverlayParams{
			ID: o.ID, Name: o.Name, CreatedAt: timeline.FormatInstant(o.CreatedAt),
		})
		if err != nil {
			return err
		}
		for i, timelineID := range o.TimelineIDs {
			err = q.AddOverlayTimeline(ctx, sqlcgen.AddOverlayTimelineParams{OverlayID: o.ID, TimelineID: timelineID, Position: int64(i + 1)})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// OverlayByName returns the overlay called name.
func (s *Store) OverlayByName(ctx context.Context, name string) (timeline.Overlay, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Overlay, error) {
		row, err := q.GetOverlayByName(ctx, name)
		if err != nil {
			return timeline.Overlay{}, err
		}

		return loadOverlay(ctx, q, row)
	})
}

// TimelineOverlays returns the overlays the timeline with timelineID is in, ordered by name.
func (s *Store) TimelineOverlays(ctx context.Context, timelineID string) ([]timeline.Overlay, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Overlay, error) {
		rows, err := q.ListTimelineOverlays(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Overlay) (timeline.Overlay, error) { return loadOverlay(ctx, q, r) })
	})
}

func loadOverlay(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Overlay) (timeline.Overlay, error) {
	timelineIDs, err := q.ListOverlayTimelineIDs(ctx, row.ID)
	if err != nil {
		return timeline.Overlay{}, err
	}

	var p instants
	o := timeline.Overlay{ID: row.ID, Name: row.Name, TimelineIDs: timelineIDs, CreatedAt: p.parse(row.CreatedAt)}

	return o, p.err
}
