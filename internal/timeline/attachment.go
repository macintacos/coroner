package timeline

import (
	"strings"
	"time"
)

// AttachmentOwner names the one event or annotation an attachment belongs to: exactly one
// of its fields is set.
type AttachmentOwner struct {
	EventID, AnnotationID *string
}

// Attachment is a file kept with one event or annotation, identified by its content's
// SHA256.
type Attachment struct {
	ID    string
	Owner AttachmentOwner
	// Filename is the name the file was attached under, for display only: never a path.
	Filename string
	// MIME is the media type detected when the file was added, possibly with parameters such
	// as charset.
	MIME string
	// SHA256 is the content's hash in 64 lowercase hex digits.
	SHA256 string
	// Size is the content's length in bytes.
	Size      int64
	CreatedAt time.Time
}

// Validate returns an error unless exactly one of EventID and AnnotationID is set.
func (o AttachmentOwner) Validate() error {
	if (o.EventID == nil) == (o.AnnotationID == nil) {
		return problem("event_id", RuleOwner, "owner: must be exactly one event or annotation")
	}

	return nil
}

// Validate reports every way a breaks the domain rules, or nil.
func (a Attachment) Validate() error {
	errs := []error{a.Owner.Validate(), ValidateSHA256(a.SHA256)}
	if a.Size < 0 {
		errs = append(errs, problem("size", RuleNegativeSize, "size %d: must not be negative", a.Size))
	}

	errs = append(errs, ValidateUTF8("filename", a.Filename), ValidateUTF8("mime", a.MIME))

	return join(errs...)
}

// ValidateSHA256 returns an error unless hash is 64 lowercase hex digits.
func ValidateSHA256(hash string) error {
	if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		return problem("sha256", RuleSHA256, "sha256 %q: must be 64 lowercase hex digits", hash)
	}

	return nil
}
