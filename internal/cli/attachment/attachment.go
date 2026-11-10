// Package attachment holds the `coroner attachment` commands.
package attachment

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/timeline"
)

const listAttachmentsHint = "Run 'coroner attachment list' to see a timeline's attachments and their ids."

// NewCommand returns the attachment command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attachment",
		Short:   "List, delete and export files attached to events and annotations",
		GroupID: clikit.CoreCommands,
		Long: "An attachment is a file, such as a screenshot, kept with one event or annotation. Attach one\n" +
			"with --attach on 'coroner event add', 'coroner event edit', 'coroner annotation add' or\n" +
			"'coroner annotation edit'. Its bytes are stored once, as a blob named by their sha256, in\n" +
			"the directory beside the database.",
		Example: "  $ coroner event edit 12 --attach ./grafana-p99.png\n" +
			"  $ coroner attachment list -e 12",
		RunE: clikit.RunNoun,
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
	prefix, ok := clikit.ParseIDPrefix(ref)
	if !ok {
		return "", &clikit.Error{Kind: clikit.KindUsage, Message: fmt.Sprintf("%q is not an attachment id", ref), Hint: listAttachmentsHint}
	}

	return prefix, nil
}

func checkAttachmentRef(ref string) error {
	_, err := parseAttachmentRef(ref)
	return err
}

// findAttachment returns the attachment whose id is, or uniquely starts with, prefix, in any
// timeline.
func findAttachment(ctx context.Context, s clikit.Records, prefix string) (timeline.Attachment, error) {
	attachments, err := s.AttachmentsByIDPrefix(ctx, prefix, clikit.ShownMatches+1)
	switch {
	case err != nil:
		return timeline.Attachment{}, err
	case len(attachments) == 1:
		return attachments[0], nil
	case len(attachments) == 0:
		return timeline.Attachment{}, &clikit.Error{
			Kind: clikit.KindAttachmentNotFound, Message: "no attachment has an id starting " + prefix, Hint: listAttachmentsHint,
		}
	}

	var rows []string
	for _, a := range attachments[:min(len(attachments), clikit.ShownMatches)] {
		rows = append(rows, fmt.Sprintf("%s  %s", a.ID, a.Filename))
	}

	return timeline.Attachment{}, clikit.AmbiguousIDError("attachment", prefix, rows, len(attachments) > clikit.ShownMatches)
}

type attachmentListJSON struct {
	Attachments []clikit.AttachmentJSON `json:"attachments"`
	Truncated   bool                    `json:"truncated"`
	Total       int                     `json:"total"`
}

// newAttachmentListJSON returns objects as a whole collection: nothing cuts one short yet.
func newAttachmentListJSON(objects []clikit.AttachmentJSON) attachmentListJSON {
	return attachmentListJSON{Attachments: objects, Truncated: false, Total: len(objects)}
}
