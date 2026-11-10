package timeline

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineEditCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name|id>",
		Short: "Rename a timeline or change its title and refs",
		Long: "Change a timeline's name, title or refs. Its id never changes, so its events, and\n" +
			"anything naming it by id, still resolve after a rename. Refs are removed before they are\n" +
			"added; adding a ref it has, or removing one it lacks, changes nothing.\n\n" +
			"On a terminal, without --name, --title, --add-ref or --remove-ref, or with --interactive, a\n" +
			"form opens on the timeline, or on the current one when none is named, applies only what you\n" +
			"change, and prints the command it equals. Under --json only --interactive opens it, and\n" +
			"--no-input never does.",
		Args:              oneArgOrNone,
		ValidArgsFunction: clikit.CompleteTimelineArg(ctx, app),
		Annotations:       map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner timeline edit mongo-outage-2026-09-11 --name mongo-failover --add-ref INC-4822\n" +
			"  $ coroner timeline edit mongo-failover --title \"\" --dry-run --json\n" +
			"  $ coroner timeline edit mongo-failover --interactive",
	}

	return EditForm(ctx, app).Attach(cmd, app, clikit.TerminalForms(app, "📒 Edit timeline"))
}

// EditForm is timeline edit with its form, which opens on a terminal when no change is
// given.
func EditForm(ctx context.Context, app clikit.App) clikit.FormCommand {
	def := EditDefinition()
	return clikit.FormCommand{
		Def:       def,
		Ready:     def.Changed,
		FlagsOnly: withOneArg(clikit.WriteRunInZone(ctx, app, def, Edit)),
		Session: func(s clikit.FormSession, in clikit.FieldValues) error {
			z, err := s.Inv.Zone()
			if err != nil {
				return err
			}

			return askTimelineEdit(ctx, s, z, in)
		},
	}
}

// timelineToEdit is the timeline an edit form opens on, as its echoed command names it, and the
// names the other timelines hold.
type timelineToEdit struct {
	timeline timeline.Timeline
	nameOrID string
	taken    []string
}

// askTimelineEdit asks for the changes to the timeline in selects, prefilled with it as the command
// line changes it, and applies what differs from the stored timeline, until it is applied, nothing
// differs, or Esc ends the form; z is the zone a dry run prints in.
func askTimelineEdit(ctx context.Context, s clikit.FormSession, z clikit.Zone, in clikit.FieldValues) error {
	target, chosen, err := chooseTimelineToEdit(ctx, s, in)
	if err != nil || !chosen {
		return err
	}
	t := target.timeline
	// The command line's changes prefill the form; they are not read otherwise.
	opening := timelineEdit{
		name: in.Value("name"), title: in.Value("title"), addRefs: in.Values("add-ref"), removeRefs: in.Values("remove-ref"),
	}.Apply(t, t.UpdatedAt)
	prefilled := clikit.FieldValues{"name": {opening.Name}, "title": {opening.Title}, "ref": opening.Refs, "dry-run": in.Values("dry-run")}

	_, _, err = s.AskUntil(ctx, EditFormDefinition(target.taken), clikit.FormChoices{}, prefilled, func(typed clikit.FieldValues) error {
		return editTyped(ctx, s, z, t.Name, EditValues(t, target.nameOrID, typed))
	})

	return err
}

// chooseTimelineToEdit returns the timeline in names, or else the one -t-less commands act on,
// asking for one when none is, and whether there is one: Esc on that ask leaves none.
func chooseTimelineToEdit(ctx context.Context, s clikit.FormSession, in clikit.FieldValues) (timelineToEdit, bool, error) {
	if !in.Given("timeline") {
		var chosen bool
		var err error
		in, chosen, err = clikit.ChooseTimeline(ctx, s, clikit.TimelinePicker{Def: EditDefinition()}, in)
		if err != nil || !chosen {
			return timelineToEdit{}, false, err
		}
	}

	nameOrID := in.Value("timeline")
	var target timelineToEdit
	err := clikit.WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		var err error
		target.timeline, err = clikit.SelectOne(ctx, s.App.Logger(), st, clikit.TimelineInput{Positional: nameOrID}, s.App.LookupEnv)
		if err != nil {
			return err
		}
		target.nameOrID = *cmp.Or(nameOrID, &target.timeline.Name)

		timelines, err := st.Timelines(ctx)
		for _, other := range timelines {
			if other.ID != target.timeline.ID {
				target.taken = append(target.taken, other.Name)
			}
		}
		return err
	})

	return target, err == nil, err
}

// EditFormDefinition is timeline edit's fields as its form asks for them: the name,
// required and noting the taken names, the title, one ref list checked as --add-ref is, in place of
// --add-ref and --remove-ref, and --dry-run.
func EditFormDefinition(taken []string) clikit.Definition {
	edit := EditDefinition()
	name := clikit.OnlyField(edit, "name").Fields[0]
	name.Required = true
	name.Preview = NamePreview(taken, func(typed string) string { return "Timeline `" + typed + "` exists; pick another name." })
	ref := clikit.OnlyField(edit, "add-ref").Fields[0]
	ref.Name, ref.List, ref.Usage = "ref", true, "Ticket keys or URLs the timeline belongs to"

	return clikit.Definition{Fields: []clikit.Field{name, clikit.OnlyField(edit, "title").Fields[0], ref, clikit.OnlyField(edit, "dry-run").Fields[0]}}
}

// EditValues returns the timeline edit values that turn t into what a form submitted: t as
// nameOrID names it, the name and title that differ, the refs added and removed, and --dry-run.
func EditValues(t timeline.Timeline, nameOrID string, submitted clikit.FieldValues) clikit.FieldValues {
	values := clikit.FieldValues{"timeline": {nameOrID}}
	if name := submitted.Value("name"); name != nil && *name != t.Name {
		values["name"] = []string{*name}
	}
	// A form gives a cleared title no value.
	if title := cmp.Or(submitted.Value("title"), new("")); *title != t.Title {
		values["title"] = []string{*title}
	}
	refs := submitted.Values("ref")
	if added := slices.DeleteFunc(slices.Clone(refs), func(ref string) bool { return slices.Contains(t.Refs, ref) }); len(added) > 0 {
		values["add-ref"] = added
	}
	if removed := slices.DeleteFunc(slices.Clone(t.Refs), func(ref string) bool { return slices.Contains(refs, ref) }); len(removed) > 0 {
		values["remove-ref"] = removed
	}
	if submitted.Given("dry-run") {
		values["dry-run"] = submitted.Values("dry-run")
	}

	return values
}

// editTyped applies the edit values given, and prints the timeline, the old name it no longer answers
// to after a rename, and the equivalent command; when values change nothing, it says so and writes
// nothing.
func editTyped(ctx context.Context, s clikit.FormSession, z clikit.Zone, oldName string, values clikit.FieldValues) error {
	if !EditDefinition().Changed(values) {
		return s.Inv.Out.Notify("Nothing to change.")
	}

	edited, err := Edit(ctx, s.App, s.Inv.Cfg, values)
	if err != nil {
		return err
	}
	edited.LogChanges(ctx, s.App.Logger())
	err = edited.Render(s.Inv.Out, z)
	if err == nil && !edited.dryRun && edited.timeline.Name != oldName {
		err = s.Inv.Out.Notify("`%s` no longer selects this timeline; commands take `%s`.", oldName, edited.timeline.Name)
	}
	if err != nil {
		return err
	}

	return s.Echo("timeline edit", EditDefinition(), values)
}

// EditDefinition declares `timeline edit`'s flags, argument and their checks.
func EditDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{Name: "timeline", Arg: true, Title: "Timeline", Usage: "Timeline to edit, as a `name|id`"},
		{Name: "name", Usage: "New `name`", Check: clikit.WithoutEnv(CheckName), Suggest: SuggestName, Changes: true},
		{Name: "title", Usage: "New `title`; an empty one clears it", Check: clikit.Utf8Only("--title"), Changes: true},
		{Name: "add-ref", Typ: clikit.StringsField, Usage: "Add a ticket key or URL; repeat for more", Check: clikit.WithoutEnv(checkAddedRef), Changes: true},
		{Name: "remove-ref", Typ: clikit.StringsField, Usage: "Remove a ref; repeat for more", Changes: true},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the edited timeline, and change nothing"},
	}}
}

// Edited is the timeline as edit stored it, or as --dry-run shows it would.
type Edited struct {
	timeline timeline.Timeline
	dryRun   bool
	fields   []string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (e Edited) LogChanges(ctx context.Context, logger *slog.Logger) {
	clikit.WithDryRun(logger, e.dryRun).InfoContext(ctx, "timeline edited", "timeline_id", e.timeline.ID, "fields", e.fields)
}

// Edit runs `timeline edit` from in's values and returns what it changed, or would change under --dry-run.
func Edit(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Edited, error) {
	err := EditDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Edited{}, err
	}
	nameOrID := in.Value("timeline")
	if nameOrID == nil {
		return Edited{}, clikit.MissingValue("timeline edit", "the timeline's name or id")
	}
	edit, err := readTimelineEdit(in)
	if err != nil {
		return Edited{}, err
	}
	dryRun, _ := in.Boolean("dry-run")

	now := app.Now()
	edited, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Edited, error) {
			t, err := clikit.FindTimeline(ctx, tx, *nameOrID)
			if err != nil {
				return Edited{}, err
			}
			before := t
			t = edit.Apply(t, now)
			err = checkTimeline(t)
			if err != nil {
				return Edited{}, err
			}
			edited := Edited{
				timeline: t, dryRun: dryRun, fields: clikit.ChangedFields(export.NewTimeline(before), export.NewTimeline(t)),
			}

			err = tx.UpdateTimeline(ctx, t)
			if errors.Is(err, store.ErrConflict) {
				return edited, timelineExistsError(t.Name, "Pick another --name.", err)
			}

			return edited, err
		})

	return edited, err
}

// Render prints the edited timeline, in z when --dry-run shows it as text.
func (e Edited) Render(o clikit.Output, z clikit.Zone) error {
	if !e.dryRun {
		err := printTimeline(o, e.timeline)
		if err != nil {
			return err
		}

		return o.Notify("Edited timeline `%s`.", e.timeline.Name)
	}

	if o.Format == clikit.FormatJSON {
		return o.JSON(clikit.NewTimelineJSON(e.timeline))
	}

	_, err := io.WriteString(o.Out, timelineSummary(e.timeline, z))
	return err
}

// timelineEdit is the change edit's fields ask for. Nil or empty fields change nothing.
type timelineEdit struct {
	name, title         *string
	addRefs, removeRefs []string
}

// readTimelineEdit returns the edit in asks for.
func readTimelineEdit(in clikit.FieldValues) (timelineEdit, error) {
	err := EditDefinition().RequireChange(in)
	if err != nil {
		return timelineEdit{}, err
	}

	return timelineEdit{
		name: in.Value("name"), title: in.Value("title"), addRefs: in.Values("add-ref"), removeRefs: in.Values("remove-ref"),
	}, nil
}

func checkAddedRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return &clikit.Error{Kind: clikit.KindUsage, Message: "--add-ref must not be blank", Hint: "A ref is a ticket key or URL."}
	}

	return clikit.CheckUTF8("--add-ref", ref)
}

// Apply returns t with the edit made and updated at set to now.
func (e timelineEdit) Apply(t timeline.Timeline, now time.Time) timeline.Timeline {
	if e.name != nil {
		t.Name = *e.name
	}
	if e.title != nil {
		t.Title = *e.title
	}

	kept := slices.DeleteFunc(slices.Clone(t.Refs), func(ref string) bool {
		return slices.Contains(e.removeRefs, ref)
	})
	t.Refs = clikit.AppendUnique(kept, e.addRefs...)
	t.UpdatedAt = now

	return t
}
