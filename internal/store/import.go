package store

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// ImportRecords is what Import writes: new records under the ids, seqs and instants they
// carry, and replacements of stored ones. A replacement keeps the stored record's seq,
// timeline and event: Import never moves a record.
type ImportRecords struct {
	Sources                             []timeline.Source
	Timelines, TimelineReplacements     []timeline.Timeline
	Events, EventReplacements           []timeline.Event
	Annotations, AnnotationReplacements []timeline.Annotation
	Attachments                         []timeline.Attachment
	Links                               []timeline.Link
}

// Import writes records in one transaction, or nothing when any write fails, and returns its
// new events, each with the seq it was written under. It keeps each new event's Seq when that
// seq is at or past its timeline's next seq, taking a timeline's new events in ascending seq,
// and moves next seq past it. Otherwise it gives the event the timeline's next seq. Every
// event and annotation names its source by name, and that source must already be stored or be
// in Sources. An attachment's blob is one of blobs with its SHA256, or one already stored. It
// returns an error wrapping ErrNotFound when a replacement names a record that is not stored,
// or when an attachment has no blob.
func (s *Store) Import(ctx context.Context, records ImportRecords, blobs ...io.Reader) ([]timeline.Event, error) {
	var inserted []timeline.Event
	_, err := s.Change(ctx, ChangeOptions{Mode: Commit, Blobs: blobs}, func(tx *Tx) error {
		var err error
		inserted, err = tx.Import(ctx, records)
		return err
	})
	if err != nil {
		return nil, err
	}

	return inserted, nil
}

// Import writes records as Store.Import does and returns its new events, each with the seq it
// was written under. An attachment's blob is the Change's staged blob with its SHA256, or one
// already stored; otherwise Import returns an error wrapping ErrNotFound.
func (tx *Tx) Import(ctx context.Context, records ImportRecords) ([]timeline.Event, error) {
	for _, err := range []error{
		validateEach(records.Sources),
		validateEach(records.Timelines),
		validateEach(records.TimelineReplacements),
		validateEach(records.Events),
		validateEach(records.EventReplacements),
		validateEach(records.Annotations),
		validateEach(records.AnnotationReplacements),
		validateEach(records.Attachments),
		validateEach(records.Links),
	} {
		if err != nil {
			return nil, err
		}
	}

	q := tx.q
	events := inSeqOrder(records.Events)
	claimed := len(tx.claimed)
	err := tx.atomic(ctx, func() error {
		for _, step := range []func() error{
			func() error { return writeEach(ctx, q, records.Sources, createSource) },
			func() error { return writeEach(ctx, q, records.Timelines, createTimeline) },
			func() error { return writeEach(ctx, q, records.TimelineReplacements, replaceTimeline) },
			func() error { return importEvents(ctx, q, events) },
			func() error { return writeEach(ctx, q, records.EventReplacements, replaceEvent) },
			func() error { return writeEach(ctx, q, records.Annotations, importAnnotation) },
			func() error { return writeEach(ctx, q, records.AnnotationReplacements, replaceAnnotation) },
			func() error { return tx.importAttachments(ctx, records.Attachments) },
			func() error { return writeEach(ctx, q, records.Links, createLink) },
		} {
			err := step()
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		tx.claimed = tx.claimed[:claimed]
		return nil, err
	}

	return events, nil
}

// importEvents writes each of events, setting its Seq to the seq it was written under.
func importEvents(ctx context.Context, q *sqlcgen.Queries, events []timeline.Event) error {
	for i, e := range events {
		written, err := importEvent(ctx, q, e)
		if err != nil {
			return err
		}
		events[i] = written
	}

	return nil
}

// validateEach returns the first of records' Validate errors.
func validateEach[T interface{ Validate() error }](records []T) error {
	for _, r := range records {
		err := r.Validate()
		if err != nil {
			return err
		}
	}

	return nil
}

// writeEach writes every one of records with write, stopping at the first error.
func writeEach[T any](
	ctx context.Context, q *sqlcgen.Queries, records []T, write func(context.Context, *sqlcgen.Queries, T) error,
) error {
	for _, r := range records {
		err := write(ctx, q, r)
		if err != nil {
			return err
		}
	}

	return nil
}

// inSeqOrder returns a copy of events sorted by timeline, then seq, with no seq (zero) last
// so it never takes a seq another event keeps.
func inSeqOrder(events []timeline.Event) []timeline.Event {
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b timeline.Event) int {
		return cmp.Or(
			strings.Compare(a.TimelineID, b.TimelineID),
			cmp.Compare(sqliteBool(a.Seq == 0), sqliteBool(b.Seq == 0)),
			cmp.Compare(a.Seq, b.Seq),
		)
	})

	return sorted
}

func createSource(ctx context.Context, q *sqlcgen.Queries, source timeline.Source) error {
	_, err := q.CreateSource(ctx, sqlcgen.CreateSourceParams{
		ID:        source.ID,
		Name:      source.Name,
		CreatedAt: timeline.FormatInstant(source.CreatedAt),
	})
	return err
}

func replaceTimeline(ctx context.Context, q *sqlcgen.Queries, t timeline.Timeline) error {
	return overwriteTimeline(ctx, q, t, new(timeline.FormatInstant(t.CreatedAt)))
}

// importEvent writes e and returns it with the seq importSeq gave it.
func importEvent(ctx context.Context, q *sqlcgen.Queries, e timeline.Event) (timeline.Event, error) {
	source, err := storedSourceID(ctx, q, e.Source)
	if err != nil {
		return timeline.Event{}, err
	}
	e.Seq, err = importSeq(ctx, q, e)
	if err != nil {
		return timeline.Event{}, err
	}

	return e, createEvent(ctx, q, e, e.Seq, source)
}

// importSeq returns e.Seq, moving its timeline's next seq past it, unless the timeline has
// already reached it, when it allocates the next seq instead.
func importSeq(ctx context.Context, q *sqlcgen.Queries, e timeline.Event) (int64, error) {
	row, err := q.GetTimeline(ctx, e.TimelineID)
	if err != nil {
		return 0, err
	}
	if e.Seq < row.NextSeq {
		return q.AllocateSeq(ctx, e.TimelineID)
	}

	return e.Seq, q.SetNextSeq(ctx, sqlcgen.SetNextSeqParams{NextSeq: e.Seq + 1, ID: e.TimelineID})
}

func replaceEvent(ctx context.Context, q *sqlcgen.Queries, e timeline.Event) error {
	source, err := storedSourceID(ctx, q, e.Source)
	if err != nil {
		return err
	}

	return overwriteEvent(ctx, q, e, source, new(timeline.FormatInstant(e.CreatedAt)))
}

func importAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation) error {
	source, err := storedSourceID(ctx, q, a.Source)
	if err != nil {
		return err
	}

	return createAnnotation(ctx, q, a, source)
}

func replaceAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation) error {
	source, err := storedSourceID(ctx, q, a.Source)
	if err != nil {
		return err
	}

	return overwriteAnnotation(ctx, q, a, source, new(timeline.FormatInstant(a.CreatedAt)))
}

// storedSourceID returns the id of the stored source called name. A nil name gives a nil id.
func storedSourceID(ctx context.Context, q *sqlcgen.Queries, name *string) (*string, error) {
	if name == nil {
		return name, nil
	}

	source, err := q.GetSourceByName(ctx, *name)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", *name, err)
	}

	return &source.ID, nil
}

// importAttachments inserts the row of each of attachments, claiming the Change's staged blob
// with its SHA256, or else requiring that blob already stored.
func (tx *Tx) importAttachments(ctx context.Context, attachments []timeline.Attachment) error {
	for _, a := range attachments {
		err := tx.claimBlob(a)
		if err != nil {
			return err
		}

		err = createAttachment(ctx, tx.q, a)
		if err != nil {
			return err
		}
	}

	return nil
}

// claimBlob appends the staged blob a carries to tx.claimed, or checks that blob is stored, and
// returns an error when a's size is not the staged blob's.
func (tx *Tx) claimBlob(a timeline.Attachment) error {
	blob, ok := tx.blobs[a.SHA256]
	if !ok {
		_, err := os.Lstat(filepath.Join(tx.blobDir, a.SHA256))
		if err != nil {
			return fmt.Errorf("attachment %s: no blob %s: %w", a.ID, a.SHA256, ErrNotFound)
		}
		return nil
	}
	if blob.size != a.Size {
		return fmt.Errorf("attachment %s: content has sha256 %s and size %d, want %s and %d",
			a.ID, blob.sha256, blob.size, a.SHA256, a.Size)
	}
	tx.claimed = append(tx.claimed, blob)

	return nil
}
