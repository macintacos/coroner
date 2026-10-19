package store

import (
	"context"
	"io"
	"os"
	"slices"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// NewAttachment is a file to attach to the event or annotation it is written with.
type NewAttachment struct {
	ID, Filename string
	CreatedAt    time.Time
	Content      io.Reader
}

// Validate reports a Filename that is not UTF-8, or nil.
func (a NewAttachment) Validate() error {
	return timeline.ValidateUTF8("filename", a.Filename)
}

// stagedAttachment is an attachment row whose content is staged in the blob directory.
type stagedAttachment struct {
	attachment timeline.Attachment
	blob       stagedBlob
}

// ListAttachments is Tx.ListAttachments in its own Read.
func (s *Store) ListAttachments(ctx context.Context, owner timeline.AttachmentOwner) ([]timeline.Attachment, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Attachment, error) { return tx.ListAttachments(ctx, owner) })
}

// ListAttachments returns the attachments of owner, oldest first.
func (tx *Tx) ListAttachments(ctx context.Context, owner timeline.AttachmentOwner) ([]timeline.Attachment, error) {
	err := owner.Validate()
	if err != nil {
		return nil, err
	}

	return translated(func() ([]timeline.Attachment, error) {
		var rows []sqlcgen.Attachment
		var err error
		if owner.EventID != nil {
			rows, err = tx.q.ListEventAttachments(ctx, owner.EventID)
		} else {
			rows, err = tx.q.ListAnnotationAttachments(ctx, owner.AnnotationID)
		}
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// Attachment returns the attachment with id.
func (tx *Tx) Attachment(ctx context.Context, id string) (timeline.Attachment, error) {
	return translated(func() (timeline.Attachment, error) {
		row, err := tx.q.GetAttachment(ctx, id)
		if err != nil {
			return timeline.Attachment{}, err
		}

		return loadAttachment(row)
	})
}

// TimelineAttachments is Tx.TimelineAttachments in its own Read.
func (s *Store) TimelineAttachments(ctx context.Context, timelineID string) ([]timeline.Attachment, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Attachment, error) { return tx.TimelineAttachments(ctx, timelineID) })
}

// TimelineAttachments returns the attachments on the events and annotations of the timeline
// with timelineID, oldest first, then by id.
func (tx *Tx) TimelineAttachments(ctx context.Context, timelineID string) ([]timeline.Attachment, error) {
	return translated(func() ([]timeline.Attachment, error) {
		rows, err := tx.q.ListTimelineAttachments(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// AttachmentsByIDPrefix is Tx.AttachmentsByIDPrefix in its own Read.
func (s *Store) AttachmentsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Attachment, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Attachment, error) { return tx.AttachmentsByIDPrefix(ctx, prefix, limit) })
}

// AttachmentsByIDPrefix returns up to limit attachments, in any timeline, whose id starts
// with prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase
// hex and dashes.
func (tx *Tx) AttachmentsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Attachment, error) {
	pattern, err := globPrefix("attachment", prefix)
	if err != nil {
		return nil, err
	}

	return translated(func() ([]timeline.Attachment, error) {
		rows, err := tx.q.ListAttachmentsByIDPrefix(ctx, sqlcgen.ListAttachmentsByIDPrefixParams{ID: pattern, Limit: int64(limit)})
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// DeleteAttachments is Tx.DeleteAttachments in its own Change.
func (s *Store) DeleteAttachments(ctx context.Context, ids ...string) error {
	return s.change(ctx, Commit, func(tx *Tx) error { return tx.DeleteAttachments(ctx, ids...) })
}

// DeleteAttachments deletes the attachments with ids, or none of them when any does not
// exist, leaving their blobs to GarbageCollectBlobs.
func (tx *Tx) DeleteAttachments(ctx context.Context, ids ...string) error {
	return tx.atomic(ctx, func() error { return deleteAll(ctx, ids, tx.q.GetAttachment, tx.q.DeleteAttachment) })
}

// GarbageCollectBlobs deletes every blob no attachment references and returns their hashes,
// sorted. Nothing else deletes a blob.
func (s *Store) GarbageCollectBlobs(ctx context.Context) ([]string, error) {
	var deleted []string
	err := s.change(ctx, Commit, func(tx *Tx) error {
		referenced, err := tx.q.ListAttachmentHashes(ctx)
		if err != nil {
			return err
		}

		deleted, err = deleteUnreferenced(s.blobDir, referenced)
		return err
	})
	if err != nil {
		return nil, err
	}

	return deleted, nil
}

func discardStaged(staged []stagedAttachment) {
	for _, a := range staged {
		// Once insertAttachments has renamed or discarded the file, the error is expected.
		_ = os.Remove(a.blob.tempPath)
	}
}

// owned returns staged with each row's Owner set to owner.
func owned(staged []stagedAttachment, owner timeline.AttachmentOwner) []stagedAttachment {
	rows := slices.Clone(staged)
	for i := range rows {
		rows[i].attachment.Owner = owner
	}

	return rows
}

// stageAttachments stages the content of each of attachments, with no owner yet, outside any
// transaction, removing what it staged when one fails.
func stageAttachments(blobDir string, attachments []NewAttachment) ([]stagedAttachment, error) {
	staged := make([]stagedAttachment, 0, len(attachments))
	for _, a := range attachments {
		blob, err := stageBlob(blobDir, a.Content)
		if err != nil {
			discardStaged(staged)
			return nil, err
		}
		staged = append(staged, stagedAttachment{
			attachment: timeline.Attachment{
				ID: a.ID, Filename: a.Filename, MIME: DetectMIME(blob.head, a.Filename),
				SHA256: blob.sha256, Size: blob.size, CreatedAt: a.CreatedAt,
			},
			blob: blob,
		})
	}

	return staged, nil
}

// insertAttachments names each staged blob by its hash and inserts its row, inside the write
// transaction q belongs to, which keeps GarbageCollectBlobs from deleting a blob before the
// row that references it commits.
func insertAttachments(ctx context.Context, q *sqlcgen.Queries, blobDir string, staged []stagedAttachment) error {
	for _, a := range staged {
		err := commitBlob(blobDir, a.blob)
		if err != nil {
			return err
		}

		err = createAttachment(ctx, q, a.attachment)
		if err != nil {
			return err
		}
	}

	return nil
}

func createAttachment(ctx context.Context, q *sqlcgen.Queries, a timeline.Attachment) error {
	_, err := q.CreateAttachment(ctx, sqlcgen.CreateAttachmentParams{
		ID:           a.ID,
		EventID:      a.Owner.EventID,
		AnnotationID: a.Owner.AnnotationID,
		Filename:     a.Filename,
		Mime:         a.MIME,
		Sha256:       a.SHA256,
		Size:         a.Size,
		CreatedAt:    timeline.FormatInstant(a.CreatedAt),
	})
	return err
}

func loadAttachment(row sqlcgen.Attachment) (timeline.Attachment, error) {
	var p instants
	a := timeline.Attachment{
		ID:        row.ID,
		Owner:     timeline.AttachmentOwner{EventID: row.EventID, AnnotationID: row.AnnotationID},
		Filename:  row.Filename,
		MIME:      row.Mime,
		SHA256:    row.Sha256,
		Size:      row.Size,
		CreatedAt: p.parse(row.CreatedAt),
	}

	return a, p.err
}
