package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAttachmentDeleteCommand(ctx context.Context, app App) *cobra.Command {
	def := attachmentDeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete attachments",
		Long: "Delete attachments by full id or unique id prefix, in any timeline, and the blobs no\n" +
			"attachment uses any more. Every id is checked before anything is deleted, so an id that\n" +
			"matches nothing deletes none. --dry-run shows what would be deleted instead.",
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner attachment delete 0199a1b2-7c1e --dry-run\n" +
			"  $ coroner attachment delete 0199a1b2-7c1e 0199a1b2-7d40 --json",
		RunE: writeRunE(ctx, app, def, deleteAttachments),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func attachmentDeleteDefinition() definition {
	return definition{fields: []field{
		{
			name: "ids", arg: true, typ: stringsField, check: withoutEnv(checkAttachmentRef),
			usage: "Attachments to delete, as `id`s or unique id prefixes",
		},
		{name: "dry-run", typ: boolField, usage: "Show what would be deleted, and delete nothing"},
	}}
}

// attachmentsDeleted are the attachments delete removed or, with --dry-run, would remove, with
// their objects and their timelines' names by attachment id.
type attachmentsDeleted struct {
	attachments   []timeline.Attachment
	objects       []attachmentJSON
	timelineNames map[string]string
	dryRun        bool
	blobsErr      error
}

func deleteAttachments(ctx context.Context, app App, cfg config.Config, in fieldValues) (attachmentsDeleted, error) {
	err := attachmentDeleteDefinition().check(in, checkEnv{})
	if err != nil {
		return attachmentsDeleted{}, err
	}
	if len(in.values("ids")) == 0 {
		return attachmentsDeleted{}, missingValue("attachment delete", "at least one attachment id")
	}
	prefixes := make([]string, 0, len(in.values("ids")))
	for _, ref := range in.values("ids") {
		prefix, err := parseAttachmentRef(ref)
		if err != nil {
			return attachmentsDeleted{}, err
		}
		prefixes = append(prefixes, prefix)
	}
	dryRun, _ := in.boolean("dry-run")

	deleted, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (attachmentsDeleted, error) {
			deleted := attachmentsDeleted{dryRun: dryRun}
			attachments, err := findAttachments(ctx, tx, prefixes)
			if err != nil {
				return deleted, err
			}
			deleted.attachments = attachments
			deleted.timelineNames, err = attachmentTimelineNames(ctx, tx, attachments)
			if err != nil {
				return deleted, err
			}

			ids := make([]string, 0, len(attachments))
			for _, a := range attachments {
				ids = append(ids, a.ID)
			}
			return deleted, tx.DeleteAttachments(ctx, ids...)
		})
	deleted.objects = newAttachmentJSONs(deleted.attachments, cfg.DBPath)
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = collectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// render prints the attachments as JSON, or says for each what was, or would be, deleted, then
// says when the blobs they leave unused could not be deleted.
func (d attachmentsDeleted) render(o output) error {
	err := d.write(o)
	if err != nil {
		return err
	}

	return notifyBlobsErr(o, d.blobsErr)
}

// notifyBlobsErr says, when err is set, that a delete could not delete the blobs it left unused.
func notifyBlobsErr(o output, err error) error {
	if err == nil {
		return nil
	}

	return o.notify("Could not delete the blobs no attachment uses any more, so the next delete will: %v", err)
}

func (d attachmentsDeleted) write(o output) error {
	if o.format == formatJSON {
		return o.json(newAttachmentListJSON(d.objects))
	}

	for _, a := range d.attachments {
		var err error
		if d.dryRun {
			_, err = fmt.Fprintf(o.out, "Would delete attachment %s (%s) from timeline `%s`\n", a.ID, a.Filename, d.timelineNames[a.ID])
		} else {
			err = o.notify("Deleted attachment %s (%s) from timeline `%s`", a.ID, a.Filename, d.timelineNames[a.ID])
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// findAttachments returns the attachment each id prefix names, in order and once each.
func findAttachments(ctx context.Context, s records, prefixes []string) ([]timeline.Attachment, error) {
	found := make([]timeline.Attachment, 0, len(prefixes))
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		a, err := findAttachment(ctx, s, prefix)
		if err != nil {
			return nil, err
		}
		if !seen[a.ID] {
			seen[a.ID] = true
			found = append(found, a)
		}
	}

	return found, nil
}

// attachmentTimelineNames returns the name of the timeline each attachment is in, keyed by
// attachment id.
func attachmentTimelineNames(ctx context.Context, s records, attachments []timeline.Attachment) (map[string]string, error) {
	names := map[string]string{}
	for _, a := range attachments {
		timelineID, err := ownerTimelineID(ctx, s, a.Owner)
		if err != nil {
			return nil, err
		}
		t, err := s.Timeline(ctx, timelineID)
		if err != nil {
			return nil, err
		}
		names[a.ID] = t.Name
	}

	return names, nil
}

// ownerTimelineID returns the id of the timeline owner's event or annotation is in.
func ownerTimelineID(ctx context.Context, s records, owner timeline.AttachmentOwner) (string, error) {
	if owner.EventID != nil {
		e, err := s.Event(ctx, *owner.EventID)
		return e.TimelineID, err
	}

	a, err := s.Annotation(ctx, *owner.AnnotationID)
	return a.TimelineID, err
}
