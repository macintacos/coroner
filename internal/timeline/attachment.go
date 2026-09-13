package timeline

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AttachmentOwner names the one event or annotation an attachment belongs to: exactly one
// of its fields is set.
type AttachmentOwner struct {
	EventID, AnnotationID *string
}

// Attachment is a file kept with an event or annotation. Its bytes are stored once per
// SHA256, however many attachments share them.
type Attachment struct {
	ID    string
	Owner AttachmentOwner
	// Filename is the name the file was attached under, for display only: never a path.
	Filename string
	MIME     string
	// SHA256 is the content's hash in 64 lowercase hex digits.
	SHA256    string
	Size      int64
	CreatedAt time.Time
}

// Validate reports every way o breaks the domain rules, or nil.
func (o AttachmentOwner) Validate() error {
	if (o.EventID == nil) == (o.AnnotationID == nil) {
		return errors.New("owner: must be exactly one event or annotation")
	}

	return nil
}

// Validate reports every way a breaks the domain rules, or nil.
func (a Attachment) Validate() error {
	errs := []error{a.Owner.Validate(), ValidateSHA256(a.SHA256)}
	if a.Size < 0 {
		errs = append(errs, fmt.Errorf("size %d: must not be negative", a.Size))
	}

	return errors.Join(errs...)
}

// ValidateSHA256 returns an error unless hash is 64 lowercase hex digits.
func ValidateSHA256(hash string) error {
	if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		return fmt.Errorf("sha256 %q: must be 64 lowercase hex digits", hash)
	}

	return nil
}
