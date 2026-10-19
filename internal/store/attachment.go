package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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

// stagedAttachment is an attachment row whose content is staged in the blob directory.
type stagedAttachment struct {
	attachment timeline.Attachment
	blob       stagedBlob
}

// AddAttachment stores content as a blob and adds a, which carries its ID, Owner, Filename and
// CreatedAt, returning a with the content's MIME, SHA256 and Size. It returns an error
// wrapping ErrNotFound when the owner does not exist, having named no blob. A crash or
// failed insert after the blob is named leaves it unreferenced until GarbageCollectBlobs,
// never a row without its blob.
func (s *Store) AddAttachment(ctx context.Context, a timeline.Attachment, content io.Reader) (timeline.Attachment, error) {
	err := a.Owner.Validate()
	if err != nil {
		return timeline.Attachment{}, err
	}

	rows, err := s.writeWithAttachments(ctx, a.Owner, []NewAttachment{
		{ID: a.ID, Filename: a.Filename, CreatedAt: a.CreatedAt, Content: content},
	}, func(q *sqlcgen.Queries) error {
		return checkOwner(ctx, q, a.Owner)
	})
	if err != nil {
		return timeline.Attachment{}, err
	}

	return rows[0], nil
}

// ListAttachments returns the attachments of owner, oldest first.
func (s *Store) ListAttachments(ctx context.Context, owner timeline.AttachmentOwner) ([]timeline.Attachment, error) {
	err := owner.Validate()
	if err != nil {
		return nil, err
	}

	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Attachment, error) {
		var rows []sqlcgen.Attachment
		var err error
		if owner.EventID != nil {
			rows, err = q.ListEventAttachments(ctx, owner.EventID)
		} else {
			rows, err = q.ListAnnotationAttachments(ctx, owner.AnnotationID)
		}
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// Attachment returns the attachment with id.
func (s *Store) Attachment(ctx context.Context, id string) (timeline.Attachment, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Attachment, error) {
		row, err := q.GetAttachment(ctx, id)
		if err != nil {
			return timeline.Attachment{}, err
		}

		return loadAttachment(row)
	})
}

// TimelineAttachments returns the attachments on the events and annotations of the timeline
// with timelineID, oldest first, then by id.
func (s *Store) TimelineAttachments(ctx context.Context, timelineID string) ([]timeline.Attachment, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Attachment, error) {
		rows, err := q.ListTimelineAttachments(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// AttachmentsByIDPrefix returns up to limit attachments, in any timeline, whose id starts
// with prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase
// hex and dashes.
func (s *Store) AttachmentsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Attachment, error) {
	if prefix == "" || strings.Trim(prefix, "0123456789abcdef-") != "" {
		return nil, fmt.Errorf("attachment id prefix %q is not lowercase hex and dashes", prefix)
	}

	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Attachment, error) {
		rows, err := q.ListAttachmentsByIDPrefix(ctx, sqlcgen.ListAttachmentsByIDPrefixParams{ID: prefix + "*", Limit: int64(limit)})
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadAttachment)
	})
}

// DeleteAttachments deletes the attachments with ids, or none of them when any does not
// exist, leaving their blobs to GarbageCollectBlobs.
func (s *Store) DeleteAttachments(ctx context.Context, ids ...string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		for _, id := range ids {
			_, err := q.GetAttachment(ctx, id)
			if err != nil {
				return err
			}
		}
		for _, id := range ids {
			err := q.DeleteAttachment(ctx, id)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// GarbageCollectBlobs deletes every blob no attachment references and returns their hashes,
// sorted. Nothing else deletes a blob.
func (s *Store) GarbageCollectBlobs(ctx context.Context) ([]string, error) {
	var deleted []string
	err := s.write(ctx, func(q *sqlcgen.Queries) error {
		referenced, err := q.ListAttachmentHashes(ctx)
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

// writeWithAttachments stages attachments as owner's outside any transaction, then, in one
// write, runs fn and names their blobs and inserts their rows after it, so a failed fn
// names no blob. It returns the rows it inserted.
func (s *Store) writeWithAttachments(
	ctx context.Context, owner timeline.AttachmentOwner, attachments []NewAttachment, fn func(*sqlcgen.Queries) error,
) ([]timeline.Attachment, error) {
	for _, a := range attachments {
		err := timeline.ValidateUTF8("filename", a.Filename)
		if err != nil {
			return nil, err
		}
	}

	staged, err := stageAttachments(s.blobDir, owner, attachments)
	if err != nil {
		return nil, err
	}

	err = s.writeStaged(ctx, staged, fn)
	if err != nil {
		return nil, err
	}

	rows := make([]timeline.Attachment, len(staged))
	for i, a := range staged {
		rows[i] = a.attachment
	}

	return rows, nil
}

// writeStaged runs fn, then names each staged blob and inserts its row after it, in one
// write, so a failed fn names no blob. It removes whatever staged file it did not name.
func (s *Store) writeStaged(ctx context.Context, staged []stagedAttachment, fn func(*sqlcgen.Queries) error) error {
	defer discardStaged(staged)

	return s.write(ctx, func(q *sqlcgen.Queries) error {
		err := fn(q)
		if err != nil {
			return err
		}

		return insertAttachments(ctx, q, s.blobDir, staged)
	})
}

func discardStaged(staged []stagedAttachment) {
	for _, a := range staged {
		// Once insertAttachments has renamed or discarded the file, the error is expected.
		_ = os.Remove(a.blob.tempPath)
	}
}

// stageAttachments stages the content of each of attachments, owned by owner, outside any
// transaction, removing what it staged when one fails.
func stageAttachments(
	blobDir string, owner timeline.AttachmentOwner, attachments []NewAttachment,
) ([]stagedAttachment, error) {
	staged := make([]stagedAttachment, 0, len(attachments))
	for _, a := range attachments {
		blob, err := stageBlob(blobDir, a.Content)
		if err != nil {
			discardStaged(staged)
			return nil, err
		}
		staged = append(staged, stagedAttachment{
			attachment: timeline.Attachment{
				ID: a.ID, Owner: owner, Filename: a.Filename, MIME: DetectMIME(blob.head, a.Filename),
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

// checkOwner returns an error wrapping ErrNotFound unless owner's event or annotation exists.
func checkOwner(ctx context.Context, q *sqlcgen.Queries, owner timeline.AttachmentOwner) error {
	if owner.EventID != nil {
		_, err := q.GetEvent(ctx, *owner.EventID)
		return err
	}

	_, err := q.GetAnnotation(ctx, *owner.AnnotationID)
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
