package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineEditCommand(ctx context.Context, app App) *cobra.Command {
	def := timelineEditDefinition()
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
		// Not writeRunE: render needs the --tz zone, which def omits as --tz changes nothing edit stores.
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := newOutput(cmd, app)
			if err != nil {
				return err
			}
			cfg, z, err := resolveZone(cmd, app)
			if err != nil {
				return err
			}
			edited, err := editTimeline(ctx, app, cfg, def.read(cmd.Flags(), args))
			if err != nil {
				return err
			}

			return edited.render(o, z)
		},
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func timelineEditDefinition() definition {
	return definition{fields: []field{
		{name: "timeline", arg: true, usage: "Timeline to edit, as a `name|id`"},
		{name: "name", usage: "New `name`", check: withoutEnv(checkName)},
		{name: "title", usage: "New `title`; an empty one clears it"},
		{name: "add-ref", typ: stringsField, usage: "Add a ticket key or URL; repeat for more", check: withoutEnv(checkAddedRef)},
		{name: "remove-ref", typ: stringsField, usage: "Remove a ref; repeat for more"},
		{name: "dry-run", typ: boolField, usage: "Show the edited timeline, and change nothing"},
	}}
}

// timelineEdited is the timeline as edit stored it, or as --dry-run shows it would.
type timelineEdited struct {
	timeline timeline.Timeline
	dryRun   bool
}

func editTimeline(ctx context.Context, app App, cfg config.Config, in fieldValues) (timelineEdited, error) {
	err := timelineEditDefinition().check(in, checkEnv{})
	if err != nil {
		return timelineEdited{}, err
	}
	nameOrID := in.value("timeline")
	if nameOrID == nil {
		return timelineEdited{}, missingValue("timeline edit", "the timeline's name or id")
	}
	edit, err := readTimelineEdit(in)
	if err != nil {
		return timelineEdited{}, err
	}
	dryRun, _ := in.boolean("dry-run")

	edited := timelineEdited{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: find and update are separate transactions, so a concurrent edit of this
		// timeline is overwritten; apply the edit inside one store write if that matters.
		t, err := findTimeline(ctx, s, *nameOrID)
		if err != nil {
			return err
		}
		t = edit.apply(t, app.Now())
		err = checkTimeline(t)
		if err != nil {
			return err
		}
		edited.timeline = t
		if dryRun {
			return checkNameFree(ctx, s, t)
		}

		err = s.UpdateTimeline(ctx, t)
		if errors.Is(err, store.ErrConflict) {
			return timelineExistsError(t.Name, "Pick another --name.", err)
		}

		return err
	})

	return edited, err
}

// render prints the edited timeline, in z when --dry-run shows it as text.
func (e timelineEdited) render(o output, z zone) error {
	if !e.dryRun {
		err := printTimeline(o, e.timeline)
		if err != nil {
			return err
		}

		return o.notify("Edited timeline `%s`.", e.timeline.Name)
	}

	if o.format == formatJSON {
		return o.json(newTimelineJSON(e.timeline))
	}

	_, err := io.WriteString(o.out, timelineSummary(e.timeline, z))
	return err
}

// timelineEdit is the change edit's fields ask for. Nil or empty fields change nothing.
type timelineEdit struct {
	name, title         *string
	addRefs, removeRefs []string
}

// readTimelineEdit returns the edit in asks for.
func readTimelineEdit(in fieldValues) (timelineEdit, error) {
	if !slices.ContainsFunc([]string{"name", "title", "add-ref", "remove-ref"}, in.given) {
		return timelineEdit{}, &Error{
			Kind:    KindUsage,
			Message: "nothing to edit",
			Hint:    "Pass --name, --title, --add-ref or --remove-ref.",
		}
	}

	return timelineEdit{
		name: in.value("name"), title: in.value("title"), addRefs: in.values("add-ref"), removeRefs: in.values("remove-ref"),
	}, nil
}

func checkAddedRef(ref string) error {
	if strings.TrimSpace(ref) != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: "--add-ref must not be blank", Hint: "A ref is a ticket key or URL."}
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

// checkNameFree fails as the update would when a timeline other than t has t's name.
func checkNameFree(ctx context.Context, s *store.Store, t timeline.Timeline) error {
	holder, err := s.TimelineByName(ctx, t.Name)
	switch {
	case err == nil && holder.ID != t.ID:
		return timelineExistsError(t.Name, "Pick another --name.", nil)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return err
	}

	return nil
}
