package clikit

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// AttachField is the --attach field of the commands that add or edit an event or annotation.
func AttachField() Field {
	return Field{
		Name: "attach", Typ: StringsField, List: true, Check: WithoutEnv(checkAttachPath),
		Usage: "Copy this `file` in as an attachment; repeat for more",
	}
}

// checkAttachPath rejects -, since stdin has no filename to attach it under, and a file whose
// name is not UTF-8, which JSON would change.
func checkAttachPath(path string) error {
	switch {
	case path == "-":
		return &Error{
			Kind: KindUsage, Message: "--attach - is not supported: stdin has no filename to attach it under",
			Hint: "Save it to a file first, then pass --attach <file>.",
		}
	case !utf8.ValidString(filepath.Base(path)):
		return &Error{Kind: KindUsage, Message: "--attach needs a file whose name is UTF-8", Hint: "Rename the file, then attach it."}
	}

	return nil
}

// AttachFile is a file --attach read, not yet stored.
type AttachFile struct {
	filename string
	Content  []byte
}

// readAttachFiles reads each file paths names, so a command rejects one it cannot read before it
// writes anything. The filename kept is the path's last element, for display only.
// ponytail: each file is read whole into memory; stream through an App.Open if large attachments
// matter.
func readAttachFiles(paths []string, readFile func(string) ([]byte, error)) ([]AttachFile, error) {
	files := make([]AttachFile, 0, len(paths))
	for _, path := range paths {
		content, err := readFile(path)
		if err != nil {
			return nil, &Error{
				Kind: KindUsage, Message: "cannot read --attach " + path, Hint: "Pass a file that exists and you can read.", Err: err,
			}
		}
		files = append(files, AttachFile{filename: filepath.Base(path), Content: content})
	}

	return files, nil
}

// newAttachments returns files as the store adds them, each with a new id, attached at now.
func newAttachments(files []AttachFile, newID func() (uuid.UUID, error), now time.Time) ([]store.NewAttachment, error) {
	attachments := make([]store.NewAttachment, 0, len(files))
	for _, f := range files {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, store.NewAttachment{
			ID: id.String(), Filename: f.filename, CreatedAt: now, Content: bytes.NewReader(f.Content),
		})
	}

	return attachments, nil
}

// describeAttachments returns files as attachments of owner at now would be stored, with no id:
// what a dry run shows.
func describeAttachments(files []AttachFile, owner timeline.AttachmentOwner, now time.Time) []timeline.Attachment {
	described := make([]timeline.Attachment, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Content)
		described = append(described, timeline.Attachment{
			Owner: owner, Filename: f.filename, MIME: store.DetectMIME(f.Content, f.filename),
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(f.Content)), CreatedAt: now,
		})
	}

	return described
}

// AttachRequest is what --attach asks an add or edit to attach.
type AttachRequest struct {
	Files []AttachFile
	// ToAdd is files as the store adds them, once staged; nil in a dry run.
	ToAdd  []store.NewAttachment
	now    time.Time
	DryRun bool
}

// Staged returns r attaching at now, with a new id for each file unless it is a dry run. A
// command stages after drafting its record, so the record's ids come first.
func (r AttachRequest) Staged(newID func() (uuid.UUID, error), now time.Time) (AttachRequest, error) {
	r.now = now
	if r.DryRun {
		return r, nil
	}

	var err error
	r.ToAdd, err = newAttachments(r.Files, newID, now)
	return r, err
}

// result returns what r attached to owner, or would attach: stored is what owner had before,
// and added the rows the Change inserted.
func (r AttachRequest) result(stored, added []timeline.Attachment, owner timeline.AttachmentOwner, dbPath string) AttachResult {
	all := slices.Concat(stored, added)
	slices.SortStableFunc(all, func(a, b timeline.Attachment) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	objects := NewAttachmentJSONs(all, dbPath)
	if !r.DryRun {
		return AttachResult{added: added, Objects: objects}
	}

	described := describeAttachments(r.Files, owner, r.now)
	for _, a := range described {
		objects = append(objects, describedAttachmentJSON(a))
	}

	return AttachResult{added: described, Objects: objects, dryRun: true}
}

// Attached is what an attach command's Change built: its result, the owner it attaches to and
// what that owner held before the Change inserted.
type Attached[T any] struct {
	Result T
	Owner  timeline.AttachmentOwner
	Stored []timeline.Attachment
}

// AttachResult returns what req attached to b's owner, given the rows the Change inserted.
func (b Attached[T]) AttachResult(req AttachRequest, inserted []timeline.Attachment, dbPath string) AttachResult {
	return req.result(b.Stored, inserted, b.Owner, dbPath)
}

// AttachIn makes owner the Change's attachment owner and b's, and keeps in b what owner holds
// before the Change inserts the staged content.
func (b *Attached[T]) AttachIn(ctx context.Context, tx *store.Tx, owner timeline.AttachmentOwner) error {
	tx.Attach(owner)
	b.Owner = owner
	var err error
	b.Stored, err = tx.ListAttachments(ctx, owner)
	return err
}

// AttachResult is what an add or edit prints about attachments.
type AttachResult struct {
	// added is what the command attached, or would attach.
	added []timeline.Attachment
	// Objects are every attachment the owner has afterwards, then those a dry run describes.
	Objects []AttachmentJSON
	dryRun  bool
}

// LogAdded logs each attachment added, through the owner's logger, which carries its dry_run.
func (r AttachResult) LogAdded(ctx context.Context, logger *slog.Logger) {
	for _, a := range r.added {
		var idArgs []any
		if a.ID != "" {
			idArgs = []any{"attachment", a.ID}
		}
		logger.InfoContext(ctx, "attachment added", slices.Concat(idArgs, OwnerArgs(a.Owner), []any{"size", a.Size, "sha256", a.SHA256})...)
	}
}

// Notify says on stderr what each added attachment is: attached, with the id to delete or export
// it by, or what a dry run would attach.
func (r AttachResult) Notify(o Output) error {
	for _, a := range r.added {
		details := fmt.Sprintf("(sha256 %s…, %s, %s)", a.SHA256[:8], graphics.ByteSize(a.Size), a.MIME)
		var err error
		if r.dryRun {
			err = o.Notify("Would attach %s %s", a.Filename, details)
		} else {
			err = o.Notify("Attached %s as %s %s", a.Filename, a.ID, details)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// AttachmentJSON is the CLI's attachment object's keys, every one always present; id and path are
// null for an attachment a dry run describes.
type AttachmentJSON struct {
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

// NewAttachmentJSONs returns the objects of stored attachments, never nil, as newAttachmentJSON
// does.
func NewAttachmentJSONs(attachments []timeline.Attachment, dbPath string) []AttachmentJSON {
	objects := make([]AttachmentJSON, 0, len(attachments))
	for _, a := range attachments {
		objects = append(objects, newAttachmentJSON(a, dbPath))
	}

	return objects
}

// newAttachmentJSON returns the object of stored attachment a, with its blob's path beside the
// database at dbPath.
func newAttachmentJSON(a timeline.Attachment, dbPath string) AttachmentJSON {
	j := describedAttachmentJSON(a)
	j.ID = new(a.ID)
	j.Path = new(store.BlobPath(dbPath, a.SHA256))

	return j
}

// describedAttachmentJSON returns the object of an attachment a dry run describes, which has no
// id or path.
func describedAttachmentJSON(a timeline.Attachment) AttachmentJSON {
	return AttachmentJSON{
		EventID:      a.Owner.EventID,
		AnnotationID: a.Owner.AnnotationID,
		Filename:     a.Filename,
		MIME:         a.MIME,
		SHA256:       a.SHA256,
		Size:         a.Size,
		CreatedAt:    timeline.FormatInstant(a.CreatedAt),
	}
}

// AttachmentsByOwner returns the objects of stored attachments keyed by the id of the event or
// annotation each belongs to.
func AttachmentsByOwner(attachments []timeline.Attachment, dbPath string) map[string][]AttachmentJSON {
	byOwner := map[string][]AttachmentJSON{}
	for _, a := range attachments {
		ownerID := *cmp.Or(a.Owner.EventID, a.Owner.AnnotationID)
		byOwner[ownerID] = append(byOwner[ownerID], newAttachmentJSON(a, dbPath))
	}

	return byOwner
}

// OwnersAttachments returns the attachments of each of owners, in owner order.
func OwnersAttachments(ctx context.Context, s Records, owners []timeline.AttachmentOwner) ([]timeline.Attachment, error) {
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

// NotifyBlobsErr says, when err is set, that a delete could not delete the blobs it left unused.
func NotifyBlobsErr(o Output, err error) error {
	if err == nil {
		return nil
	}

	return o.Notify("Could not delete the blobs no attachment uses any more, so the next delete will: %v", err)
}

// ReadAttachRequest reads every file --attach names, which a command does before it reads stdin
// or opens the store.
func ReadAttachRequest(in FieldValues, readFile func(string) ([]byte, error)) (AttachRequest, error) {
	files, err := readAttachFiles(in.Values("attach"), readFile)
	dryRun, _ := in.Boolean("dry-run")

	return AttachRequest{Files: files, DryRun: dryRun}, err
}
