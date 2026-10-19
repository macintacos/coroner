package cli

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const listAttachmentsHint = "Run 'coroner attachment list' to see a timeline's attachments and their ids."

func newAttachmentCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attachment",
		Short:   "List, delete and export files attached to events and annotations",
		GroupID: coreCommands,
		Long: "An attachment is a file, such as a screenshot, kept with one event or annotation. Attach one\n" +
			"with --attach on 'coroner event add', 'coroner event edit', 'coroner annotation add' or\n" +
			"'coroner annotation edit'. Its bytes are stored once, as a blob named by their sha256, in\n" +
			"the directory beside the database.",
		Example: "  $ coroner event edit 12 --attach ./grafana-p99.png\n" +
			"  $ coroner attachment list -e 12",
		RunE: runNoun,
	}

	cmd.AddCommand(
		newAttachmentListCommand(ctx, app),
		newAttachmentExportCommand(ctx, app),
		newAttachmentDeleteCommand(ctx, app),
	)

	return cmd
}

// parseAttachmentRef returns ref as an attachment id prefix, lowercased.
func parseAttachmentRef(ref string) (string, error) {
	prefix, ok := parseIDPrefix(ref)
	if !ok {
		return "", &Error{Kind: KindUsage, Message: fmt.Sprintf("%q is not an attachment id", ref), Hint: listAttachmentsHint}
	}

	return prefix, nil
}

func checkAttachmentRef(ref string) error {
	_, err := parseAttachmentRef(ref)
	return err
}

// findAttachment returns the attachment whose id is, or uniquely starts with, prefix, in any
// timeline.
func findAttachment(ctx context.Context, s records, prefix string) (timeline.Attachment, error) {
	attachments, err := s.AttachmentsByIDPrefix(ctx, prefix, shownMatches+1)
	switch {
	case err != nil:
		return timeline.Attachment{}, err
	case len(attachments) == 1:
		return attachments[0], nil
	case len(attachments) == 0:
		return timeline.Attachment{}, &Error{
			Kind: KindAttachmentNotFound, Message: "no attachment has an id starting " + prefix, Hint: listAttachmentsHint,
		}
	}

	var rows []string
	for _, a := range attachments[:min(len(attachments), shownMatches)] {
		rows = append(rows, fmt.Sprintf("%s  %s", a.ID, a.Filename))
	}

	return timeline.Attachment{}, ambiguousIDError("attachment", prefix, rows, len(attachments) > shownMatches)
}

type attachmentListJSON struct {
	Attachments []attachmentJSON `json:"attachments"`
	Truncated   bool             `json:"truncated"`
	Total       int              `json:"total"`
}

// newAttachmentListJSON returns objects as a whole collection: nothing cuts one short yet.
func newAttachmentListJSON(objects []attachmentJSON) attachmentListJSON {
	return attachmentListJSON{Attachments: objects, Truncated: false, Total: len(objects)}
}

// attachField is the --attach field of the commands that add or edit an event or annotation.
func attachField() field {
	return field{
		name: "attach", typ: stringsField, list: true, check: withoutEnv(checkAttachPath),
		usage: "Copy this `file` in as an attachment; repeat for more",
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

// attachFile is a file --attach read, not yet stored.
type attachFile struct {
	filename string
	content  []byte
}

// readAttachFiles reads each file paths names, so a command rejects one it cannot read before it
// writes anything. The filename kept is the path's last element, for display only.
// ponytail: each file is read whole into memory; stream through an App.Open if large attachments
// matter.
func readAttachFiles(paths []string, readFile func(string) ([]byte, error)) ([]attachFile, error) {
	files := make([]attachFile, 0, len(paths))
	for _, path := range paths {
		content, err := readFile(path)
		if err != nil {
			return nil, &Error{
				Kind: KindUsage, Message: "cannot read --attach " + path, Hint: "Pass a file that exists and you can read.", Err: err,
			}
		}
		files = append(files, attachFile{filename: filepath.Base(path), content: content})
	}

	return files, nil
}

// newAttachments returns files as the store adds them, each with a new id, attached at now.
func newAttachments(files []attachFile, newID func() (uuid.UUID, error), now time.Time) ([]store.NewAttachment, error) {
	attachments := make([]store.NewAttachment, 0, len(files))
	for _, f := range files {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, store.NewAttachment{
			ID: id.String(), Filename: f.filename, CreatedAt: now, Content: bytes.NewReader(f.content),
		})
	}

	return attachments, nil
}

// describeAttachments returns files as attachments of owner at now would be stored, with no id:
// what a dry run shows.
func describeAttachments(files []attachFile, owner timeline.AttachmentOwner, now time.Time) []timeline.Attachment {
	described := make([]timeline.Attachment, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.content)
		described = append(described, timeline.Attachment{
			Owner: owner, Filename: f.filename, MIME: store.DetectMIME(f.content, f.filename),
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(f.content)), CreatedAt: now,
		})
	}

	return described
}

// attachRequest is what --attach asks an add or edit to attach.
type attachRequest struct {
	files []attachFile
	// toAdd is files as the store adds them, once staged; nil in a dry run.
	toAdd  []store.NewAttachment
	now    time.Time
	dryRun bool
}

// readAttachRequest reads every file --attach names, which a command does before it reads stdin
// or opens the store.
func readAttachRequest(in fieldValues, readFile func(string) ([]byte, error)) (attachRequest, error) {
	files, err := readAttachFiles(in.values("attach"), readFile)
	dryRun, _ := in.boolean("dry-run")

	return attachRequest{files: files, dryRun: dryRun}, err
}

// staged returns r attaching at now, with a new id for each file unless it is a dry run. A
// command stages after drafting its record, so the record's ids come first.
func (r attachRequest) staged(newID func() (uuid.UUID, error), now time.Time) (attachRequest, error) {
	r.now = now
	if r.dryRun {
		return r, nil
	}

	var err error
	r.toAdd, err = newAttachments(r.files, newID, now)
	return r, err
}

// result returns what r attached to owner, or would attach: stored is what owner had before,
// and added the rows the Change inserted.
func (r attachRequest) result(stored, added []timeline.Attachment, owner timeline.AttachmentOwner, dbPath string) attachResult {
	all := slices.Concat(stored, added)
	slices.SortStableFunc(all, func(a, b timeline.Attachment) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	objects := newAttachmentJSONs(all, dbPath)
	if !r.dryRun {
		return attachResult{added: added, objects: objects}
	}

	described := describeAttachments(r.files, owner, r.now)
	for _, a := range described {
		objects = append(objects, describedAttachmentJSON(a))
	}

	return attachResult{added: described, objects: objects, dryRun: true}
}

// attached is what an attach command's Change built: its result, the owner it attaches to and
// what that owner held before the Change inserted.
type attached[T any] struct {
	result T
	owner  timeline.AttachmentOwner
	stored []timeline.Attachment
}

// attachResult returns what req attached to b's owner, given the rows the Change inserted.
func (b attached[T]) attachResult(req attachRequest, inserted []timeline.Attachment, dbPath string) attachResult {
	return req.result(b.stored, inserted, b.owner, dbPath)
}

// attachIn makes owner the Change's attachment owner and b's, and keeps in b what owner holds
// before the Change inserts the staged content.
func (b *attached[T]) attachIn(ctx context.Context, tx *store.Tx, owner timeline.AttachmentOwner) error {
	tx.Attach(owner)
	b.owner = owner
	var err error
	b.stored, err = tx.ListAttachments(ctx, owner)
	return err
}

// attachResult is what an add or edit prints about attachments.
type attachResult struct {
	// added is what the command attached, or would attach.
	added []timeline.Attachment
	// objects are every attachment the owner has afterwards, then those a dry run describes.
	objects []attachmentJSON
	dryRun  bool
}

// notify says on stderr what each added attachment is: attached, with the id to delete or export
// it by, or what a dry run would attach.
func (r attachResult) notify(o output) error {
	for _, a := range r.added {
		details := fmt.Sprintf("(sha256 %s…, %s, %s)", a.SHA256[:8], graphics.ByteSize(a.Size), a.MIME)
		var err error
		if r.dryRun {
			err = o.notify("Would attach %s %s", a.Filename, details)
		} else {
			err = o.notify("Attached %s as %s %s", a.Filename, a.ID, details)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// attachmentJSON is the CLI's attachment object's keys, every one always present; id and path are
// null for an attachment a dry run describes.
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

// newAttachmentJSONs returns the objects of stored attachments, never nil, as newAttachmentJSON
// does.
func newAttachmentJSONs(attachments []timeline.Attachment, dbPath string) []attachmentJSON {
	objects := make([]attachmentJSON, 0, len(attachments))
	for _, a := range attachments {
		objects = append(objects, newAttachmentJSON(a, dbPath))
	}

	return objects
}

// newAttachmentJSON returns the object of stored attachment a, with its blob's path beside the
// database at dbPath.
func newAttachmentJSON(a timeline.Attachment, dbPath string) attachmentJSON {
	j := describedAttachmentJSON(a)
	j.ID = new(a.ID)
	j.Path = new(store.BlobPath(dbPath, a.SHA256))

	return j
}

// describedAttachmentJSON returns the object of an attachment a dry run describes, which has no
// id or path.
func describedAttachmentJSON(a timeline.Attachment) attachmentJSON {
	return attachmentJSON{
		EventID:      a.Owner.EventID,
		AnnotationID: a.Owner.AnnotationID,
		Filename:     a.Filename,
		MIME:         a.MIME,
		SHA256:       a.SHA256,
		Size:         a.Size,
		CreatedAt:    timeline.FormatInstant(a.CreatedAt),
	}
}

// attachmentsByOwner returns the objects of stored attachments keyed by the id of the event or
// annotation each belongs to.
func attachmentsByOwner(attachments []timeline.Attachment, dbPath string) map[string][]attachmentJSON {
	byOwner := map[string][]attachmentJSON{}
	for _, a := range attachments {
		ownerID := *cmp.Or(a.Owner.EventID, a.Owner.AnnotationID)
		byOwner[ownerID] = append(byOwner[ownerID], newAttachmentJSON(a, dbPath))
	}

	return byOwner
}

// ownersAttachments returns the attachments of each of owners, in owner order.
func ownersAttachments(ctx context.Context, s records, owners []timeline.AttachmentOwner) ([]timeline.Attachment, error) {
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
