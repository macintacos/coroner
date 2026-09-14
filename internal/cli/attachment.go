package cli

import (
	"cmp"
	"context"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// attachmentJSON is the canonical attachment object's keys, every one always present. id and
// path are null for an attachment a dry run describes, which has neither yet.
type attachmentJSON struct {
	ID           *string `json:"id"`
	EventID      *string `json:"event_id"`
	AnnotationID *string `json:"annotation_id"`
	Filename     string  `json:"filename"`
	MIME         string  `json:"mime"`
	SHA256       string  `json:"sha256"`
	Size         int64   `json:"size"`
	Path         *string `json:"path"`
	CreatedAt    string  `json:"created_at"`
}

// newAttachmentJSONs returns the objects of attachments, never nil, each stored one with its
// blob's path beside the database at dbPath. An attachment with no ID is one a dry run describes.
func newAttachmentJSONs(attachments []timeline.Attachment, dbPath string) []attachmentJSON {
	objects := make([]attachmentJSON, 0, len(attachments))
	for _, a := range attachments {
		j := attachmentJSON{
			EventID:      a.Owner.EventID,
			AnnotationID: a.Owner.AnnotationID,
			Filename:     a.Filename,
			MIME:         a.MIME,
			SHA256:       a.SHA256,
			Size:         a.Size,
			CreatedAt:    timeline.FormatInstant(a.CreatedAt),
		}
		if a.ID != "" {
			j.ID = new(a.ID)
			j.Path = new(store.BlobPath(dbPath, a.SHA256))
		}
		objects = append(objects, j)
	}

	return objects
}

// attachmentsByOwner returns the objects of attachments, as newAttachmentJSONs does, keyed by
// the id of the event or annotation each belongs to.
func attachmentsByOwner(attachments []timeline.Attachment, dbPath string) map[string][]attachmentJSON {
	byOwner := map[string][]attachmentJSON{}
	for i, a := range attachments {
		owner := *cmp.Or(a.Owner.EventID, a.Owner.AnnotationID)
		byOwner[owner] = append(byOwner[owner], newAttachmentJSONs(attachments[i:i+1], dbPath)...)
	}

	return byOwner
}

// ownersAttachments returns the attachments of each of owners, in owner order.
func ownersAttachments(ctx context.Context, s *store.Store, owners []timeline.AttachmentOwner) ([]timeline.Attachment, error) {
	var attachments []timeline.Attachment
	for _, owner := range owners {
		owned, err := s.ListAttachments(ctx, owner)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, owned...)
	}

	return attachments, nil
}
