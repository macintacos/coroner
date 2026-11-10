package event

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const linkKindHint = "Pass --kind led_to, --kind same_as or --kind copied_from."

func newEventLinkCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := LinkDefinition()
	cmd := &cobra.Command{
		Use:   "link <from-event> <to-event> --kind {led_to | same_as | copied_from}",
		Short: "Link one event to another, in any timeline",
		Long: "Link the first event to the second. --kind says how they relate: led_to when the first led\n" +
			"to the second, same_as when both record one moment as two sources saw it, and copied_from\n" +
			"when the first was copied from the second. The events may be in different timelines; an\n" +
			"event cannot link to itself. From one event to another there is at most one link of each\n" +
			"kind, so another exits 4; a link in the opposite direction is a separate link. --dry-run\n" +
			"checks the link and adds nothing. 'coroner event view' shows an event's links, and exports\n" +
			"carry every link between two exported events.\n\n" + clikit.EventRefHelp,
		Args:        cobra.ExactArgs(2),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner event link mongo-failover#12 checkout-latency#3 --kind led_to\n" +
			"  $ coroner event link 0199a1b2-7c1e 0199a1b2-7d40 --kind same_as --json",
		RunE: clikit.WriteRunE(ctx, app, def, Link),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

func newEventUnlinkCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := UnlinkDefinition()
	cmd := &cobra.Command{
		Use:   "unlink <from-event> <to-event> --kind {led_to | same_as | copied_from}",
		Short: "Remove the link of a kind from one event to another",
		Long: "Remove the link of --kind from the first event to the second, and print it as it was. A link\n" +
			"that does not exist exits 3. --dry-run shows the link it would remove instead.\n\n" + clikit.EventRefHelp,
		Args:        cobra.ExactArgs(2),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --dry-run\n" +
			"  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --json",
		RunE: clikit.WriteRunE(ctx, app, def, Unlink),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// LinkDefinition declares `event link`'s flags, argument and their checks.
func LinkDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.Inherited(clikit.TimelineField()),
		linkEventsField(),
		linkKindField(),
		{Name: "author", Usage: "Who is adding it, as a `name` (default: config author, then USER)", Check: clikit.FreeText("--author")},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the link as it would be added, and add nothing"},
	}}
}

// UnlinkDefinition declares `event unlink`'s flags, argument and their checks.
func UnlinkDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.Inherited(clikit.TimelineField()),
		linkEventsField(),
		linkKindField(),
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the link that would be removed, and remove nothing"},
	}}
}

func linkEventsField() clikit.Field {
	return clikit.Field{
		Name: "events", Arg: true, Typ: clikit.StringsField, Check: clikit.WithoutEnv(clikit.CheckEventRef),
		Usage: "The event the link leaves, then the one it reaches, as `ref`s: seq, timeline#seq or id",
	}
}

func linkKindField() clikit.Field {
	return clikit.Field{Name: "kind", Usage: "Link `kind`: {led_to | same_as | copied_from}", Check: clikit.WithoutEnv(checkLinkKind)}
}

func checkLinkKind(kind string) error {
	if timeline.LinkKind(kind).Valid() {
		return nil
	}

	return &clikit.Error{Kind: clikit.KindUsage, Message: fmt.Sprintf("--kind %q is not led_to, same_as or copied_from", kind), Hint: linkKindHint}
}

// LinkChange is the link a link or unlink command added or removed or, with --dry-run, would,
// with the events it joins.
type LinkChange struct {
	link            timeline.Link
	ends            linkEnds
	dryRun, created bool
	// verb, done, preposition and logged say what happens to the link: "link", "Linked", "to" and
	// "link added", or "unlink", "Unlinked", "from" and "link deleted".
	verb, done, preposition, logged string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (c LinkChange) LogChanges(ctx context.Context, logger *slog.Logger) {
	clikit.WithDryRun(logger, c.dryRun).InfoContext(ctx, c.logged,
		"link", c.link.ID, "kind", c.link.Kind, "from_event", c.link.FromEventID, "to_event", c.link.ToEventID)
}

// linkEnds are the two events a link joins, from then to, with their timelines.
type linkEnds struct {
	events    [2]timeline.Event
	timelines [2]timeline.Timeline
}

func (e linkEnds) ref(i int) string {
	return clikit.SeqRef(e.timelines[i], e.events[i])
}

// Link runs `event link` from in's values and returns what it changed, or would change under --dry-run.
func Link(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (LinkChange, error) {
	refs, kind, err := readLinkRequest("event link", LinkDefinition(), in)
	if err != nil {
		return LinkChange{}, err
	}
	id, err := app.NewID()
	if err != nil {
		return LinkChange{}, err
	}
	author, err := clikit.DefaultAuthor(in.Value("author"), cfg.Author, app.LookupEnv)
	if err != nil {
		return LinkChange{}, err
	}
	dryRun, _ := in.Boolean("dry-run")
	now := app.Now()

	change, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (LinkChange, error) {
			change := LinkChange{
				link:   timeline.Link{ID: id.String(), Kind: kind, CreatedAt: now, Author: author},
				dryRun: dryRun, verb: "link", done: "Linked", preposition: "to", logged: "link added",
			}
			ends, err := findLinkEnds(ctx, app.Logger(), tx, refs, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return change, err
			}
			change.ends = ends
			change.link.FromEventID, change.link.ToEventID = ends.events[0].ID, ends.events[1].ID
			err = checkLink(change.link, ends)
			if err != nil {
				return change, err
			}

			err = tx.AddLink(ctx, change.link)
			if errors.Is(err, store.ErrConflict) {
				return change, linkExistsError(ends, kind, err)
			}
			change.created = err == nil
			return change, err
		})

	return change, err
}

// Unlink runs `event unlink` from in's values and returns what it changed, or would change under --dry-run.
func Unlink(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (LinkChange, error) {
	refs, kind, err := readLinkRequest("event unlink", UnlinkDefinition(), in)
	if err != nil {
		return LinkChange{}, err
	}
	dryRun, _ := in.Boolean("dry-run")
	change, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (LinkChange, error) {
			change := LinkChange{dryRun: dryRun, verb: "unlink", done: "Unlinked", preposition: "from", logged: "link deleted"}
			ends, err := findLinkEnds(ctx, app.Logger(), tx, refs, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return change, err
			}
			change.ends = ends
			err = checkLink(timeline.Link{FromEventID: ends.events[0].ID, ToEventID: ends.events[1].ID, Kind: kind}, ends)
			if err != nil {
				return change, err
			}
			change.link, err = tx.LinkBetween(ctx, ends.events[0].ID, ends.events[1].ID, kind)
			if err == nil {
				err = tx.DeleteLink(ctx, change.link.ID)
			}
			if errors.Is(err, store.ErrNotFound) {
				return change, linkNotFoundError(ends, kind, err)
			}
			return change, err
		})

	return change, err
}

// readLinkRequest checks in against def and returns the two event refs and the kind it gives,
// or usage naming command when either is missing.
func readLinkRequest(command string, def clikit.Definition, in clikit.FieldValues) ([2]clikit.EventRef, timeline.LinkKind, error) {
	var refs [2]clikit.EventRef
	err := def.Check(in, clikit.CheckEnv{})
	if err != nil {
		return refs, "", err
	}
	values := in.Values("events")
	if len(values) != len(refs) {
		return refs, "", clikit.MissingValue(command, "two event refs, the event the link leaves and the one it reaches")
	}
	kind := in.Value("kind")
	if kind == nil {
		return refs, "", clikit.MissingValue(command, "--kind")
	}
	for i, value := range values {
		refs[i], err = clikit.ParseEventRef(value)
		if err != nil {
			return refs, "", err
		}
	}

	return refs, timeline.LinkKind(*kind), nil
}

// findLinkEnds finds the events refs name.
func findLinkEnds(
	ctx context.Context, logger *slog.Logger, s clikit.Records, refs [2]clikit.EventRef, timelines clikit.TimelineInput, lookupEnv func(string) (string, bool),
) (linkEnds, error) {
	var ends linkEnds
	for i, ref := range refs {
		var err error
		ends.events[i], ends.timelines[i], err = clikit.FindEvent(ctx, logger, s, ref, timelines, lookupEnv)
		if err != nil {
			return ends, err
		}
	}

	return ends, nil
}

// checkLink returns a self link between ends as usage naming the event, and any other rule l
// breaks as the kind its rule names.
func checkLink(l timeline.Link, ends linkEnds) error {
	err := l.Validate()
	if p, ok := clikit.FirstProblem(err); ok && p.Rule == timeline.RuleSelfLink {
		return &clikit.Error{
			Kind: clikit.KindOf(p.Rule), Message: fmt.Sprintf("`%s` cannot link to itself", ends.ref(0)), Hint: "Pass two different events.", Err: err,
		}
	}

	return clikit.RecordError(err, "")
}

func linkExistsError(ends linkEnds, kind timeline.LinkKind, err error) *clikit.Error {
	return &clikit.Error{
		Kind: clikit.KindLinkExists, Message: fmt.Sprintf("`%s` already links to `%s` as %s", ends.ref(0), ends.ref(1), kind),
		Hint: fmt.Sprintf("Run 'coroner event view %s' to see its links.", ends.ref(0)), Err: err,
	}
}

func linkNotFoundError(ends linkEnds, kind timeline.LinkKind, err error) *clikit.Error {
	return &clikit.Error{
		Kind: clikit.KindLinkNotFound, Message: fmt.Sprintf("`%s` does not link to `%s` as %s", ends.ref(0), ends.ref(1), kind),
		Hint: fmt.Sprintf("Run 'coroner event view %s' to see its links.", ends.ref(0)), Err: err,
	}
}

// Render prints the result in o's format.
func (c LinkChange) Render(o clikit.Output) error {
	if o.Format == clikit.FormatJSON {
		err := o.JSON(export.NewLink(c.link))
		if err != nil {
			return err
		}
	}

	did := c.done
	switch {
	case c.dryRun:
		did = "Would " + c.verb
	case c.created:
		err := o.PrintID(c.link.ID)
		if err != nil {
			return err
		}
	}

	return o.Notify("%s `%s` %s `%s` as %s.", did, c.ends.ref(0), c.preposition, c.ends.ref(1), c.link.Kind)
}
