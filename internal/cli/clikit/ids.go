package clikit

import "github.com/google/uuid"

// ReplayIDs returns draw's ids, giving a re-run of the same work the ids its first run drew, so
// a Change that re-runs its fn keeps CORONER_TEST_IDS' order.
type ReplayIDs struct {
	Draw  func() (uuid.UUID, error)
	drawn []uuid.UUID
	next  int
}

// NewID returns the next id drawn before the last Rewind, or draws a new one.
func (r *ReplayIDs) NewID() (uuid.UUID, error) {
	if r.next < len(r.drawn) {
		r.next++
		return r.drawn[r.next-1], nil
	}

	id, err := r.Draw()
	if err != nil {
		return id, err
	}
	r.drawn = append(r.drawn, id)
	r.next++

	return id, nil
}

// Rewind makes the next NewID return the first id drawn. Call it first in every run of fn, or a
// re-run draws new ids.
func (r *ReplayIDs) Rewind() {
	r.next = 0
}
