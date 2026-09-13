package cli

import (
	"cmp"
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
	cmd := &cobra.Command{
		Use:   "edit <name|id>",
		Short: "Rename a timeline or change its title and refs",
		Long: "Change a timeline's name, title or refs. Its id never changes, so its events, and\n" +
			"anything naming it by id, still resolve after a rename. Refs are removed before they are\n" +
			"added; adding a ref it has, or removing one it lacks, changes nothing.\n\n" +
			"On a terminal, with no flags or with --interactive, a form opens on the timeline, or on the\n" +
			"current one when none is named, applies only what you change, and prints the command it\n" +
			"equals.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline edit mongo-outage-2026-09-11 --name mongo-failover --add-ref INC-4822\n" +
			"  $ coroner timeline edit mongo-failover --title \"\" --dry-run --json\n" +
			"  $ coroner timeline edit mongo-failover --interactive",
	}

	return timelineEditForm(ctx, app).attach(cmd, app, terminalForms(app))
}

// timelineEditForm is timeline edit with its form, which opens on a terminal when no change is
// given.
func timelineEditForm(ctx context.Context, app App) formCommand {
	def := timelineEditDefinition()
	return formCommand{
		def:   def,
		ready: func(in fieldValues) bool { return slices.ContainsFunc(timelineChanges(), in.given) },
		// Not writeRunE: render needs the --tz zone, which def omits as --tz changes nothing edit stores.
		flagsOnly: withOneArg(func(cmd *cobra.Command, args []string) error {
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
		}),
		session: func(cmd *cobra.Command, s formSession, in fieldValues) error {
			_, z, err := resolveZone(cmd, app)
			if err != nil {
				return err
			}

			return askTimelineEdit(ctx, s, z, in)
		},
	}
}

// timelineChanges are the fields of timeline edit that change a timeline.
func timelineChanges() []string {
	return []string{"name", "title", "add-ref", "remove-ref"}
}

// timelineToEdit is the timeline an edit form opens on, as its echoed command names it, and the
// names the other timelines hold.
type timelineToEdit struct {
	timeline timeline.Timeline
	nameOrID string
	taken    []string
}

// askTimelineEdit asks for the changes to the timeline in selects, prefilled with it as the command
// line changes it, and applies what differs from the stored timeline, until that is applied or Esc
// ends the form. A rejection it can correct asks again with what was typed. z shows a dry run's
// times.
func askTimelineEdit(ctx context.Context, s formSession, z zone, in fieldValues) error {
	edited, chosen, err := chooseTimelineToEdit(ctx, s, in)
	if err != nil || !chosen {
		return err
	}
	t := edited.timeline
	// The command line's changes prefill the form; they are not read otherwise.
	opening := timelineEdit{
		name: in.value("name"), title: in.value("title"), addRefs: in.values("add-ref"), removeRefs: in.values("remove-ref"),
	}.apply(t, t.UpdatedAt)
	typed := fieldValues{"name": {opening.Name}, "title": {opening.Title}, "refs": opening.Refs, "dry-run": in.values("dry-run")}
	def := timelineEditFormDefinition(edited.taken)

	for {
		fields := def.formFields(baseCheckEnv(s), typed, formChoices{})
		submitted, err := s.ask(ctx, fields...)
		switch {
		case err != nil:
			return askError(err)
		case !submitted:
			return nil
		}

		typed = def.formValues(fields)
		done, err := editTyped(ctx, s, z, t.Name, timelineEditValues(t, edited.nameOrID, typed))
		if done || err != nil {
			return err
		}
	}
}

// chooseTimelineToEdit returns the timeline in names, or else the one -t-less commands act on,
// asking for one when none is, and whether there is one: Esc on that ask leaves none.
func chooseTimelineToEdit(ctx context.Context, s formSession, in fieldValues) (timelineToEdit, bool, error) {
	if !in.given("timeline") {
		var chosen bool
		var err error
		in, chosen, err = chooseTimeline(ctx, s, timelineEditDefinition(), in)
		if err != nil || !chosen {
			return timelineToEdit{}, false, err
		}
	}

	var edited timelineToEdit
	err := withStorePath(ctx, s.app, s.cfg.DBPath, func(st *store.Store) error {
		var err error
		if nameOrID := in.value("timeline"); nameOrID != nil {
			edited.timeline, err = findTimeline(ctx, st, *nameOrID)
		} else {
			edited.timeline, err = selectTimeline(ctx, st, nil, s.app.LookupEnv)
		}
		if err != nil {
			return err
		}
		edited.nameOrID = edited.timeline.Name
		if nameOrID := in.value("timeline"); nameOrID != nil {
			edited.nameOrID = *nameOrID
		}

		timelines, err := st.Timelines(ctx)
		for _, other := range timelines {
			if other.ID != edited.timeline.ID {
				edited.taken = append(edited.taken, other.Name)
			}
		}
		return err
	})

	return edited, err == nil, err
}

// timelineEditFormDefinition is timeline edit's fields as its form asks for them: the name,
// required and noting the taken names, the title, one refs list in place of --add-ref and
// --remove-ref, and --dry-run.
func timelineEditFormDefinition(taken []string) definition {
	edit := timelineEditDefinition()
	name := onlyField(edit, "name").fields[0]
	name.required = true
	name.preview = namePreview(taken, "Timeline `%s` exists; pick another name.")

	return definition{fields: []field{
		name,
		onlyField(edit, "title").fields[0],
		{name: "refs", typ: stringsField, list: true, usage: "Ticket keys or URLs the timeline belongs to"},
		onlyField(edit, "dry-run").fields[0],
	}}
}

// timelineEditValues returns the timeline edit values that turn t into what a form submitted: t as
// nameOrID names it, the name and title that differ, the refs added and removed, and --dry-run.
func timelineEditValues(t timeline.Timeline, nameOrID string, submitted fieldValues) fieldValues {
	values := fieldValues{"timeline": {nameOrID}}
	if name := submitted.value("name"); name != nil && *name != t.Name {
		values["name"] = []string{*name}
	}
	// A form gives a cleared title no value.
	if title := cmp.Or(submitted.value("title"), new("")); *title != t.Title {
		values["title"] = []string{*title}
	}
	refs := submitted.values("refs")
	if added := slices.DeleteFunc(slices.Clone(refs), func(ref string) bool { return slices.Contains(t.Refs, ref) }); len(added) > 0 {
		values["add-ref"] = added
	}
	if removed := slices.DeleteFunc(slices.Clone(t.Refs), func(ref string) bool { return slices.Contains(refs, ref) }); len(removed) > 0 {
		values["remove-ref"] = removed
	}
	if submitted.given("dry-run") {
		values["dry-run"] = submitted.values("dry-run")
	}

	return values
}

// editTyped applies the edit values give, prints the timeline, the old name it no longer answers to
// after a rename, and the equivalent command, and reports whether the form is done; when it is not,
// the form asks again with what was typed.
func editTyped(ctx context.Context, s formSession, z zone, oldName string, values fieldValues) (bool, error) {
	if !slices.ContainsFunc(timelineChanges(), values.given) {
		return true, s.o.notify("Nothing to change.")
	}

	edited, err := editTimeline(ctx, s.app, s.cfg, values)
	rejected, _ := errors.AsType[*Error](err)
	switch {
	case rejected != nil && (correctable(err) || rejected.Kind == KindTimelineExists):
		return false, writeError(s.app.Err, rejected, s.style)
	case err != nil:
		return true, err
	}

	err = edited.render(s.o, z)
	if err == nil && !edited.dryRun && edited.timeline.Name != oldName {
		err = s.o.notify("`%s` no longer selects this timeline; commands take `%s`.", oldName, edited.timeline.Name)
	}
	if err != nil {
		return true, err
	}

	return true, s.echo("timeline edit", timelineEditDefinition(), values)
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
	if !slices.ContainsFunc(timelineChanges(), in.given) {
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
