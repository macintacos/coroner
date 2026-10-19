package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// CreateOverlay is Tx.CreateOverlay in its own Change.
func (s *Store) CreateOverlay(ctx context.Context, o timeline.Overlay) error {
	return s.change(ctx, Commit, nil, func(tx *Tx) error { return tx.CreateOverlay(ctx, o) })
}

// CreateOverlay saves o with its timelines, kept in the order given. It returns an error
// wrapping ErrConflict when another overlay has o's name.
func (tx *Tx) CreateOverlay(ctx context.Context, o timeline.Overlay) error {
	err := o.Validate()
	if err != nil {
		return err
	}

	return tx.atomic(ctx, func() error {
		_, err := tx.q.CreateOverlay(ctx, sqlcgen.CreateOverlayParams{
			ID: o.ID, Name: o.Name, CreatedAt: timeline.FormatInstant(o.CreatedAt),
		})
		if err != nil {
			return err
		}
		for i, timelineID := range o.TimelineIDs {
			err = tx.q.AddOverlayTimeline(ctx, sqlcgen.AddOverlayTimelineParams{OverlayID: o.ID, TimelineID: timelineID, Position: int64(i + 1)})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// OverlayByName is Tx.OverlayByName in its own Read.
func (s *Store) OverlayByName(ctx context.Context, name string) (timeline.Overlay, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Overlay, error) { return tx.OverlayByName(ctx, name) })
}

// OverlayByName returns the overlay called name.
func (tx *Tx) OverlayByName(ctx context.Context, name string) (timeline.Overlay, error) {
	return translated(func() (timeline.Overlay, error) {
		row, err := tx.q.GetOverlayByName(ctx, name)
		if err != nil {
			return timeline.Overlay{}, err
		}

		return loadOverlay(ctx, tx.q, row)
	})
}

// TimelineOverlays is Tx.TimelineOverlays in its own Read.
func (s *Store) TimelineOverlays(ctx context.Context, timelineID string) ([]timeline.Overlay, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Overlay, error) { return tx.TimelineOverlays(ctx, timelineID) })
}

// TimelineOverlays returns the overlays the timeline with timelineID is in, ordered by name.
func (tx *Tx) TimelineOverlays(ctx context.Context, timelineID string) ([]timeline.Overlay, error) {
	return translated(func() ([]timeline.Overlay, error) {
		rows, err := tx.q.ListTimelineOverlays(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Overlay) (timeline.Overlay, error) { return loadOverlay(ctx, tx.q, r) })
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
