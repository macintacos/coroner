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
// carry, and replacements of stored ones.
type ImportRecords struct {
	Sources                             []timeline.Source
	Timelines, TimelineReplacements     []timeline.Timeline
	Events, EventReplacements           []timeline.Event
	Annotations, AnnotationReplacements []timeline.Annotation
	Attachments                         []ImportAttachment
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
// must already be stored or be in Sources. It returns an error wrapping ErrNotFound when an
// attachment with nil Content has no blob.
func (s *Store) Import(ctx context.Context, records ImportRecords) error {
	staged, discard, err := stageImportAttachments(s.blobDir, records.Attachments)
	if err != nil {
		return err
	}
	defer discard()

	events := inSeqOrder(records.Events)
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		for _, step := range []func() error{
			func() error { return each(ctx, q, records.Sources, createSource) },
			func() error { return each(ctx, q, records.Timelines, createTimeline) },
			func() error { return each(ctx, q, records.TimelineReplacements, replaceTimeline) },
			func() error { return each(ctx, q, events, importEvent) },
			func() error { return each(ctx, q, records.EventReplacements, replaceEvent) },
			func() error { return each(ctx, q, records.Annotations, importAnnotation) },
			func() error { return each(ctx, q, records.AnnotationReplacements, replaceAnnotation) },
			func() error { return insertAttachments(ctx, q, s.blobDir, staged) },
			func() error { return insertStoredBlobAttachments(ctx, q, s.blobDir, records.Attachments) },
		} {
			err := step()
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// each writes every one of records with write, stopping at the first error.
func each[T any](
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
// transaction. On failure it removes what it staged; otherwise discard removes whatever
// insertAttachments did not name, so the caller defers it.
func stageImportAttachments(blobDir string, attachments []ImportAttachment) ([]stagedAttachment, func(), error) {
	staged := make([]stagedAttachment, 0, len(attachments))
	discard := func() {
		for _, a := range staged {
			// Once insertAttachments has renamed or discarded the file, the error is expected.
			_ = os.Remove(a.blob.tempPath)
		}
	}

	for _, a := range attachments {
		if a.Content == nil {
			continue
		}
		blob, err := stageMatchingBlob(blobDir, a)
		if err != nil {
			discard()
			return nil, nil, err
		}
		staged = append(staged, stagedAttachment{attachment: a.Attachment, blob: blob})
	}

	return staged, discard, nil
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

// inSeqOrder returns a copy of events sorted by timeline, then seq.
func inSeqOrder(events []timeline.Event) []timeline.Event {
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b timeline.Event) int {
		return cmp.Or(strings.Compare(a.TimelineID, b.TimelineID), cmp.Compare(a.Seq, b.Seq))
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

func replaceTimeline(ctx context.Context, q *sqlcgen.Queries, t timeline.Timeline) error {
	err := q.ReplaceTimeline(ctx, sqlcgen.ReplaceTimelineParams{
		Name:      t.Name,
		Title:     t.Title,
		CreatedAt: timeline.FormatInstant(t.CreatedAt),
		UpdatedAt: timeline.FormatInstant(t.UpdatedAt),
		ID:        t.ID,
	})
	if err != nil {
		return err
	}

	return replaceRefs(ctx, q, t.ID, t.Refs)
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

	_, err = q.CreateEvent(ctx, sqlcgen.CreateEventParams{
		ID:                e.ID,
		TimelineID:        e.TimelineID,
		Seq:               seq,
		OccurredAt:        timeline.FormatInstant(e.OccurredAt),
		OccurredOffset:    offsetParam(e.OccurredOffset),
		OccurredPrecision: string(e.Precision),
		Approximate:       sqliteBool(e.Approximate),
		OccurredRaw:       e.OccurredRaw,
		EndedAt:           formatOptional(e.EndedAt),
		SourceID:          source,
		Text:              e.Text,
		Url:               e.URL,
		Author:            e.Author,
		CreatedAt:         timeline.FormatInstant(e.CreatedAt),
		UpdatedAt:         timeline.FormatInstant(e.UpdatedAt),
	})
	if err != nil {
		return err
	}

	return addTags(ctx, q, e.ID, e.Tags)
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

	err = q.ReplaceEvent(ctx, sqlcgen.ReplaceEventParams{
		OccurredAt:        timeline.FormatInstant(e.OccurredAt),
		OccurredOffset:    offsetParam(e.OccurredOffset),
		OccurredPrecision: string(e.Precision),
		Approximate:       sqliteBool(e.Approximate),
		OccurredRaw:       e.OccurredRaw,
		EndedAt:           formatOptional(e.EndedAt),
		SourceID:          source,
		Text:              e.Text,
		Url:               e.URL,
		Author:            e.Author,
		CreatedAt:         timeline.FormatInstant(e.CreatedAt),
		UpdatedAt:         timeline.FormatInstant(e.UpdatedAt),
		ID:                e.ID,
	})
	if err != nil {
		return err
	}

	return replaceTags(ctx, q, e.ID, e.Tags)
}

func importAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation) error {
	source, err := storedSourceID(ctx, q, a.Source)
	if err != nil {
		return err
	}

	_, err = q.CreateAnnotation(ctx, sqlcgen.CreateAnnotationParams{
		ID:         a.ID,
		TimelineID: a.TimelineID,
		EventID:    a.EventID,
		Kind:       string(a.Kind),
		Body:       a.Body,
		Author:     a.Author,
		SourceID:   source,
		Url:        a.URL,
		CreatedAt:  timeline.FormatInstant(a.CreatedAt),
		UpdatedAt:  timeline.FormatInstant(a.UpdatedAt),
	})
	return err
}

func replaceAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation) error {
	source, err := storedSourceID(ctx, q, a.Source)
	if err != nil {
		return err
	}

	return q.ReplaceAnnotation(ctx, sqlcgen.ReplaceAnnotationParams{
		Kind:      string(a.Kind),
		Body:      a.Body,
		Author:    a.Author,
		SourceID:  source,
		Url:       a.URL,
		CreatedAt: timeline.FormatInstant(a.CreatedAt),
		UpdatedAt: timeline.FormatInstant(a.UpdatedAt),
		ID:        a.ID,
	})
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
