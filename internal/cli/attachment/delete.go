package attachment

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAttachmentDeleteCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := DeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete attachments",
		Long: "Delete attachments by full id or unique id prefix, in any timeline, and the blobs no\n" +
			"attachment uses any more. Every id is checked before anything is deleted, so an id that\n" +
			"matches nothing deletes none. --dry-run shows what would be deleted instead.",
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner attachment delete 0199a1b2-7c1e --dry-run\n" +
			"  $ coroner attachment delete 0199a1b2-7c1e 0199a1b2-7d40 --json",
		RunE: clikit.WriteRunE(ctx, app, def, Delete),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// DeleteDefinition declares `attachment delete`'s flags, argument and their checks.
func DeleteDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{
			Name: "ids", Arg: true, Typ: clikit.StringsField, Check: clikit.WithoutEnv(checkAttachmentRef),
			Usage: "Attachments to delete, as `id`s or unique id prefixes",
		},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show what would be deleted, and delete nothing"},
	}}
}

// Deleted are the attachments delete removed or, with --dry-run, would remove, with
// their objects and their timelines' names by attachment id.
type Deleted struct {
	attachments   []timeline.Attachment
	objects       []clikit.AttachmentJSON
	timelineNames map[string]string
	dryRun        bool
	blobsErr      error
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (d Deleted) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, d.dryRun)
	for _, a := range d.attachments {
		logger.InfoContext(ctx, "attachment deleted", append([]any{"attachment", a.ID}, clikit.OwnerArgs(a.Owner)...)...)
	}
}

// Delete runs `attachment delete` from in's values and returns what it changed, or would change under --dry-run.
func Delete(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Deleted, error) {
	err := DeleteDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Deleted{}, err
	}
	if len(in.Values("ids")) == 0 {
		return Deleted{}, clikit.MissingValue("attachment delete", "at least one attachment id")
	}
	prefixes := make([]string, 0, len(in.Values("ids")))
	for _, ref := range in.Values("ids") {
		prefix, err := parseAttachmentRef(ref)
		if err != nil {
			return Deleted{}, err
		}
		prefixes = append(prefixes, prefix)
	}
	dryRun, _ := in.Boolean("dry-run")

	deleted, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Deleted, error) {
			deleted := Deleted{dryRun: dryRun}
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
	deleted.objects = clikit.NewAttachmentJSONs(deleted.attachments, cfg.DBPath)
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = clikit.CollectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// Render prints the attachments as JSON, or says for each what was, or would be, deleted, then
// says when the blobs they leave unused could not be deleted.
func (d Deleted) Render(o clikit.Output) error {
	err := d.write(o)
	if err != nil {
		return err
	}

	return clikit.NotifyBlobsErr(o, d.blobsErr)
}

func (d Deleted) write(o clikit.Output) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(newAttachmentListJSON(d.objects))
	}

	for _, a := range d.attachments {
		var err error
		if d.dryRun {
			_, err = fmt.Fprintf(o.Out, "Would delete attachment %s (%s) from timeline `%s`\n", a.ID, a.Filename, d.timelineNames[a.ID])
		} else {
			err = o.Notify("Deleted attachment %s (%s) from timeline `%s`", a.ID, a.Filename, d.timelineNames[a.ID])
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// findAttachments returns the attachment each id prefix names, in order and once each.
func findAttachments(ctx context.Context, s clikit.Records, prefixes []string) ([]timeline.Attachment, error) {
	return clikit.FindEach(prefixes, clikit.AttachmentID, func(prefix string) (timeline.Attachment, error) {
		return findAttachment(ctx, s, prefix)
	})
}

// attachmentTimelineNames returns the name of the timeline each attachment is in, keyed by
// attachment id.
func attachmentTimelineNames(ctx context.Context, s clikit.Records, attachments []timeline.Attachment) (map[string]string, error) {
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
func ownerTimelineID(ctx context.Context, s clikit.Records, owner timeline.AttachmentOwner) (string, error) {
	if owner.EventID != nil {
		e, err := s.Event(ctx, *owner.EventID)
		return e.TimelineID, err
	}

	a, err := s.Annotation(ctx, *owner.AnnotationID)
	return a.TimelineID, err
}
