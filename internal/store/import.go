package store

import (
	"cmp"
	"context"
	"errors"
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
	Attachments                         []ImportAttachment
	Links                               []timeline.Link
}

// ImportAttachment is an attachment row to insert, with its content, or nil Content when the
// blob directory already holds its SHA256.
type ImportAttachment struct {
	timeline.Attachment
	Content io.Reader
}

// Import writes records in one transaction, or nothing when any write fails. It keeps each
// new event's Seq when that seq is at or past its timeline's next seq, taking a timeline's
// new events in ascending seq, and moves next seq past it. Otherwise it gives the event the
// timeline's next seq. Every event and annotation names its source by name, and that source
// must already be stored or be in Sources. It returns an error wrapping ErrNotFound when a
// replacement names a record that is not stored, or when an attachment with nil Content has
// no blob.
func (s *Store) Import(ctx context.Context, records ImportRecords) error {
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
			return err
		}
	}

	staged, err := stageImportAttachments(s.blobDir, records.Attachments)
	if err != nil {
		return err
	}

	events := inSeqOrder(records.Events)
	return s.writeStaged(ctx, staged, func(q *sqlcgen.Queries) error {
		for _, step := range []func() error{
			func() error { return writeEach(ctx, q, records.Sources, createSource) },
			func() error { return writeEach(ctx, q, records.Timelines, createTimeline) },
			func() error { return writeEach(ctx, q, records.TimelineReplacements, replaceTimeline) },
			func() error { return writeEach(ctx, q, events, importEvent) },
			func() error { return writeEach(ctx, q, records.EventReplacements, replaceEvent) },
			func() error { return writeEach(ctx, q, records.Annotations, importAnnotation) },
			func() error { return writeEach(ctx, q, records.AnnotationReplacements, replaceAnnotation) },
			func() error { return insertStoredBlobAttachments(ctx, q, s.blobDir, records.Attachments) },
			func() error { return writeEach(ctx, q, records.Links, createLink) },
		} {
			err := step()
			if err != nil {
				return err
			}
		}

		return nil
	})
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

// stageImportAttachments stages the content of each attachment that has it, outside any
// transaction, removing what it staged when one fails.
func stageImportAttachments(blobDir string, attachments []ImportAttachment) ([]stagedAttachment, error) {
	staged := make([]stagedAttachment, 0, len(attachments))
	for _, a := range attachments {
		if a.Content == nil {
			continue
		}
		blob, err := stageMatchingBlob(blobDir, a)
		if err != nil {
			discardStaged(staged)
			return nil, err
		}
		staged = append(staged, stagedAttachment{attachment: a.Attachment, blob: blob})
	}

	return staged, nil
}

// stageMatchingBlob stages a's content, removing it again when its hash or size is not a's.
func stageMatchingBlob(blobDir string, a ImportAttachment) (stagedBlob, error) {
	blob, err := stageBlob(blobDir, a.Content)
	if err != nil {
		return stagedBlob{}, err
	}
	if blob.sha256 != a.SHA256 || blob.size != a.Size {
		return stagedBlob{}, errors.Join(
			fmt.Errorf("attachment %s: content has sha256 %s and size %d, want %s and %d",
				a.ID, blob.sha256, blob.size, a.SHA256, a.Size),
			os.Remove(blob.tempPath),
		)
	}

	return blob, nil
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

func importEvent(ctx context.Context, q *sqlcgen.Queries, e timeline.Event) error {
	source, err := storedSourceID(ctx, q, e.Source)
	if err != nil {
		return err
	}
	seq, err := importSeq(ctx, q, e)
	if err != nil {
		return err
	}

	return createEvent(ctx, q, e, seq, source)
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

// insertStoredBlobAttachments inserts the row of each of attachments with nil Content,
// returning an error wrapping ErrNotFound when its blob is not in blobDir.
func insertStoredBlobAttachments(
	ctx context.Context, q *sqlcgen.Queries, blobDir string, attachments []ImportAttachment,
) error {
	for _, a := range attachments {
		if a.Content != nil {
			continue
		}
		_, err := os.Lstat(filepath.Join(blobDir, a.SHA256))
		if err != nil {
			return fmt.Errorf("attachment %s: no blob %s: %w", a.ID, a.SHA256, ErrNotFound)
		}

		err = createAttachment(ctx, q, a.Attachment)
		if err != nil {
			return err
		}
	}

	return nil
}
