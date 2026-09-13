package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline edit mongo-outage-2026-09-11 --name mongo-failover --add-ref INC-4822\n" +
			"  $ coroner timeline edit mongo-failover --title \"\" --dry-run --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			edit, err := readTimelineEdit(cmd.Flags())
			if err != nil {
				return err
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				// ponytail: read, then write in a second transaction, so a concurrent edit of the
				// same timeline's refs can be lost; apply the edit inside one store write if that matters.
				t, err := findTimeline(ctx, s, args[0])
				if err != nil {
					return err
				}
				t = edit.apply(t, app.Now())
				err = checkTimeline(t)
				if err != nil {
					return err
				}

				if edit.dryRun {
					return printDryRun(cmd, app, t)
				}
				err = updateTimeline(ctx, s, t)
				if err != nil {
					return err
				}

				return printChanged(cmd, app, t, fmt.Sprintf("Edited timeline `%s`.\n", t.Name))
			})
		},
	}

	cmd.Flags().String("name", "", "New `name`")
	cmd.Flags().String("title", "", "New `title`; an empty one clears it")
	cmd.Flags().StringArray("add-ref", nil, "Add a ticket key or URL; repeat for more")
	cmd.Flags().StringArray("remove-ref", nil, "Remove a ref; repeat for more")
	cmd.Flags().Bool("dry-run", false, "Show the edited timeline, and change nothing")

	return cmd
}

// timelineEdit is the change edit's flags ask for. A nil field leaves that field alone.
type timelineEdit struct {
	name, title         *string
	addRefs, removeRefs []string
	dryRun              bool
}

func readTimelineEdit(flags *pflag.FlagSet) (timelineEdit, error) {
	addRefs, _ := flags.GetStringArray("add-ref")
	removeRefs, _ := flags.GetStringArray("remove-ref")
	dryRun, _ := flags.GetBool("dry-run")
	edit := timelineEdit{
		name:       changedString(flags, "name"),
		title:      changedString(flags, "title"),
		addRefs:    addRefs,
		removeRefs: removeRefs,
		dryRun:     dryRun,
	}

	if edit.name == nil && edit.title == nil && len(addRefs) == 0 && len(removeRefs) == 0 {
		return edit, &Error{
			Kind:    KindUsage,
			Message: "nothing to edit",
			Hint:    "Pass --name, --title, --add-ref or --remove-ref.",
		}
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
	t.Refs = appendRefs(kept, e.addRefs...)
	t.UpdatedAt = now

	return t
}

func updateTimeline(ctx context.Context, s *store.Store, t timeline.Timeline) error {
	err := s.UpdateTimeline(ctx, t)
	if errors.Is(err, store.ErrConflict) {
		return &Error{
			Kind:    KindTimelineExists,
			Message: fmt.Sprintf("timeline %q already exists", t.Name),
			Hint:    "Pick another --name.",
			Err:     err,
		}
	}

	return err
}

// printDryRun prints t as it would be stored: as JSON when cmd was asked for it, otherwise
// as its summary.
func printDryRun(cmd *cobra.Command, app App, t timeline.Timeline) error {
	if printsJSON(cmd) {
		return writeJSON(app.Out, app.OutIsTTY, newTimelineJSON(t))
	}

	_, err := io.WriteString(app.Out, timelineSummary(t))
	return err
}
