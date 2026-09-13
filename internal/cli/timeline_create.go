package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineCreateCommand(ctx context.Context, app App) *cobra.Command {
	def := timelineCreateDefinition()
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a timeline",
		Long: "Create a timeline. Its name is how commands select it: at most 64 lowercase letters,\n" +
			"digits, '.', '_' or '-', starting with a letter or digit. Put free text in --title.\n" +
			"Creating a timeline does not make it current; run 'coroner timeline use' for that.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --title \"Primary election storm\" \\\n" +
			"      --ref INC-4821 --ref https://jira.example.com/browse/INC-4821\n" +
			"  $ coroner timeline create mongo-outage-2026-09-11 --if-not-exists --json",
		RunE: writeRunE(ctx, app, def, createTimeline),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func timelineCreateDefinition() definition {
	return definition{fields: []field{
		{name: "name", usage: "Timeline `name`, which commands select it by", arg: true, check: withoutEnv(checkName)},
		{name: "title", usage: "Free-text `title`"},
		{name: "ref", typ: stringsField, usage: "Ticket key or URL the timeline belongs to; repeat for more"},
		{name: "if-not-exists", typ: boolField, usage: "Print the existing timeline instead of failing when the name is taken"},
	}}
}

// timelineCreated is the timeline create made, or the one --if-not-exists found.
type timelineCreated struct {
	timeline timeline.Timeline
	existed  bool
}

func createTimeline(ctx context.Context, app App, cfg config.Config, in fieldValues) (timelineCreated, error) {
	// Before the checks, so a rejected name still uses an ID, as the contract scripts number them.
	id, err := app.NewID()
	if err != nil {
		return timelineCreated{}, err
	}
	err = timelineCreateDefinition().check(in, checkEnv{})
	if err != nil {
		return timelineCreated{}, err
	}
	now := app.Now()
	t := timeline.Timeline{
		ID: id.String(), Name: in.values("name")[0], Refs: appendUnique(nil, in.values("ref")...),
		CreatedAt: now, UpdatedAt: now,
	}
	if title := in.value("title"); title != nil {
		t.Title = *title
	}
	err = checkTimeline(t)
	if err != nil {
		return timelineCreated{}, err
	}
	ifNotExists, _ := in.boolean("if-not-exists")

	created := timelineCreated{timeline: t}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		err := s.CreateTimeline(ctx, t)
		switch {
		case errors.Is(err, store.ErrConflict) && ifNotExists:
			created.timeline, err = s.TimelineByName(ctx, t.Name)
			created.existed = true
			return err
		case errors.Is(err, store.ErrConflict):
			return timelineExistsError(t.Name, "Pick another name, or pass --if-not-exists to use the existing timeline.", err)
		}

		return err
	})

	return created, err
}

func (c timelineCreated) render(o output) error {
	err := printTimeline(o, c.timeline)
	if err != nil {
		return err
	}
	if c.existed {
		return o.notify("Timeline `%s` already exists.", c.timeline.Name)
	}

	return o.notify("Created timeline `%[1]s`.\nNext: coroner timeline use %[1]s\n      coroner event add -t %[1]s <text>", c.timeline.Name)
}
