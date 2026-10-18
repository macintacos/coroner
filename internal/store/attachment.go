package store

import (
	"context"
	"io"
	"os"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

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

	staged, err := stageBlob(s.blobDir, content)
	if err != nil {
		return timeline.Attachment{}, err
	}
	// Removes the staged file on every failure; once commitBlob has renamed or discarded it,
	// the error is expected.
	defer func() { _ = os.Remove(staged.tempPath) }()

	a.MIME = detectMIME(staged.head, a.Filename)
	a.SHA256 = staged.sha256
	a.Size = staged.size

	// Naming the blob inside the write transaction keeps GarbageCollectBlobs, which holds the
	// same lock, from deleting it before the row that references it commits.
	err = s.write(ctx, func(q *sqlcgen.Queries) error {
		err := checkOwner(ctx, q, a.Owner)
		if err != nil {
			return err
		}
		err = commitBlob(s.blobDir, staged)
		if err != nil {
			return err
		}

		_, err = q.CreateAttachment(ctx, sqlcgen.CreateAttachmentParams{
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
	})
	if err != nil {
		return timeline.Attachment{}, err
	}

	return a, nil
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

// RemoveAttachment removes the attachment with id, leaving its blob to GarbageCollectBlobs.
func (s *Store) RemoveAttachment(ctx context.Context, id string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetAttachment(ctx, id)
		if err != nil {
			return err
		}

		return q.DeleteAttachment(ctx, id)
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
