package cli

import "github.com/google/uuid"

// replayIDs returns draw's ids, giving a re-run of the same work the ids its first run drew, so
// a Change that re-runs its fn keeps CORONER_TEST_IDS' order.
type replayIDs struct {
	draw  func() (uuid.UUID, error)
	drawn []uuid.UUID
	next  int
}

func (r *replayIDs) newID() (uuid.UUID, error) {
	if r.next < len(r.drawn) {
		r.next++
		return r.drawn[r.next-1], nil
	}

	id, err := r.draw()
	if err != nil {
		return id, err
	}
	r.drawn = append(r.drawn, id)
	r.next++

	return id, nil
}

// rewind starts a re-run, which fn does first.
func (r *replayIDs) rewind() {
	r.next = 0
}
