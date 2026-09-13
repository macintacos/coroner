package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineEditCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name|id>",
		Short: "Rename a timeline or change its title and refs",
		Long: "Change a timeline's name, title or refs. Its id never changes, so its events, and\n" +
			"anything naming it by id, still resolve after a rename. Refs are removed before they are\n" +
			"added; adding a ref it has, or removing one it lacks, changes nothing.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline edit mongo-outage-2026-09-11 --name mongo-failover --add-ref INC-4822\n" +
			"  $ coroner timeline edit mongo-failover --title \"\" --dry-run --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return editTimeline(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().String("name", "", "New `name`")
	cmd.Flags().String("title", "", "New `title`; an empty one clears it")
	cmd.Flags().StringArray("add-ref", nil, "Add a ticket key or URL; repeat for more")
	cmd.Flags().StringArray("remove-ref", nil, "Remove a ref; repeat for more")
	cmd.Flags().Bool("dry-run", false, "Show the edited timeline, and change nothing")

	return cmd
}

func editTimeline(ctx context.Context, cmd *cobra.Command, app App, nameOrID string) error {
	edit, err := readTimelineEdit(cmd.Flags())
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: find and update are separate transactions, so a concurrent edit of this
		// timeline is overwritten; apply the edit inside one store write if that matters.
		t, err := findTimeline(ctx, s, nameOrID)
		if err != nil {
			return err
		}
		t = edit.apply(t, app.Now())
		err = checkTimeline(t)
		if err != nil {
			return err
		}
		if dryRun {
			return previewEdit(ctx, s, o, z, t)
		}

		err = s.UpdateTimeline(ctx, t)
		if errors.Is(err, store.ErrConflict) {
			return timelineExistsError(t.Name, "Pick another --name.", err)
		}
		if err != nil {
			return err
		}
		err = printTimeline(o, t)
		if err != nil {
			return err
		}

		return o.notify("Edited timeline `%s`.", t.Name)
	})
}

// timelineEdit is the change edit's flags ask for. Nil or empty fields change nothing.
type timelineEdit struct {
	name, title         *string
	addRefs, removeRefs []string
}

// readTimelineEdit returns the edit the flags ask for. It runs before the store opens, so it
// rejects a name or ref no timeline could take.
func readTimelineEdit(flags *pflag.FlagSet) (timelineEdit, error) {
	edit := timelineEdit{
		name:       changedString(flags, "name"),
		title:      changedString(flags, "title"),
		addRefs:    stringArray(flags, "add-ref"),
		removeRefs: stringArray(flags, "remove-ref"),
	}

	if !slices.ContainsFunc([]string{"name", "title", "add-ref", "remove-ref"}, flags.Changed) {
		return edit, &Error{
			Kind:    KindUsage,
			Message: "nothing to edit",
			Hint:    "Pass --name, --title, --add-ref or --remove-ref.",
		}
	}
	if edit.name != nil {
		err := checkName(*edit.name)
		if err != nil {
			return edit, err
		}
	}
	if slices.ContainsFunc(edit.addRefs, func(ref string) bool { return strings.TrimSpace(ref) == "" }) {
		return edit, &Error{Kind: KindUsage, Message: "--add-ref must not be blank", Hint: "A ref is a ticket key or URL."}
	}

	return edit, nil
}

// apply returns t with the edit made and updated at set to now.
func (e timelineEdit) apply(t timeline.Timeline, now time.Time) timeline.Timeline {
	if e.name != nil {
		t.Name = *e.name
	}
	if e.title != nil {
		t.Title = *e.title
	}

	kept := slices.DeleteFunc(slices.Clone(t.Refs), func(ref string) bool {
		return slices.Contains(e.removeRefs, ref)
	})
	t.Refs = appendUnique(kept, e.addRefs...)
	t.UpdatedAt = now

	return t
}

// previewEdit prints t as the edit would store it, with times in z, failing as the update
// would when another timeline has t's name.
func previewEdit(ctx context.Context, s *store.Store, o output, z zone, t timeline.Timeline) error {
	holder, err := s.TimelineByName(ctx, t.Name)
	switch {
	case err == nil && holder.ID != t.ID:
		return timelineExistsError(t.Name, "Pick another --name.", nil)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return err
	}

	if o.format == formatJSON {
		return o.json(newTimelineJSON(t))
	}

	_, err = io.WriteString(o.out, timelineSummary(t, z))
	return err
}
