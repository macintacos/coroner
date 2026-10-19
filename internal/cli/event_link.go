package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const linkKindHint = "Pass --kind led_to, --kind same_as or --kind copied_from."

func newEventLinkCommand(ctx context.Context, app App) *cobra.Command {
	def := eventLinkDefinition()
	cmd := &cobra.Command{
		Use:   "link <from-event> <to-event> --kind {led_to | same_as | copied_from}",
		Short: "Link one event to another, in any timeline",
		Long: "Link the first event to the second. --kind says how they relate: led_to when the first led\n" +
			"to the second, same_as when both record one moment as two sources saw it, and copied_from\n" +
			"when the first was copied from the second. The events may be in different timelines; an\n" +
			"event cannot link to itself. From one event to another there is at most one link of each\n" +
			"kind, so another exits 4; a link in the opposite direction is a separate link. --dry-run\n" +
			"checks the link and adds nothing. 'coroner event view' shows an event's links, and exports\n" +
			"carry every link between two exported events.\n\n" + eventRefHelp,
		Args:        cobra.ExactArgs(2),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event link mongo-failover#12 checkout-latency#3 --kind led_to\n" +
			"  $ coroner event link 0199a1b2-7c1e 0199a1b2-7d40 --kind same_as --json",
		RunE: writeRunE(ctx, app, def, linkEvents),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func newEventUnlinkCommand(ctx context.Context, app App) *cobra.Command {
	def := eventUnlinkDefinition()
	cmd := &cobra.Command{
		Use:   "unlink <from-event> <to-event> --kind {led_to | same_as | copied_from}",
		Short: "Remove the link of a kind from one event to another",
		Long: "Remove the link of --kind from the first event to the second, and print it as it was. A link\n" +
			"that does not exist exits 3. --dry-run shows the link it would remove instead.\n\n" + eventRefHelp,
		Args:        cobra.ExactArgs(2),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --dry-run\n" +
			"  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --json",
		RunE: writeRunE(ctx, app, def, unlinkEvents),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func eventLinkDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		linkEventsField(),
		linkKindField(),
		{name: "author", usage: "Who is adding it, as a `name` (default: config author, then USER)", check: freeText("--author")},
		{name: "dry-run", typ: boolField, usage: "Show the link as it would be added, and add nothing"},
	}}
}

func eventUnlinkDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		linkEventsField(),
		linkKindField(),
		{name: "dry-run", typ: boolField, usage: "Show the link that would be removed, and remove nothing"},
	}}
}

func linkEventsField() field {
	return field{
		name: "events", arg: true, typ: stringsField, check: withoutEnv(checkEventRef),
		usage: "The event the link leaves, then the one it reaches, as `ref`s: seq, timeline#seq or id",
	}
}

func linkKindField() field {
	return field{name: "kind", usage: "Link `kind`: {led_to | same_as | copied_from}", check: withoutEnv(checkLinkKind)}
}

func checkLinkKind(kind string) error {
	if timeline.LinkKind(kind).Valid() {
		return nil
	}

	return &Error{Kind: KindUsage, Message: fmt.Sprintf("--kind %q is not led_to, same_as or copied_from", kind), Hint: linkKindHint}
}

// linkChange is the link a link or unlink command added or removed or, with --dry-run, would,
// with the events it joins.
type linkChange struct {
	link            timeline.Link
	ends            linkEnds
	dryRun, created bool
	// verb, done and preposition say what happens to the link: "link", "Linked" and "to", or
	// "unlink", "Unlinked" and "from".
	verb, done, preposition string
}

// linkEnds are the two events a link joins, from then to, with their timelines.
type linkEnds struct {
	events    [2]timeline.Event
	timelines [2]timeline.Timeline
}

func (e linkEnds) ref(i int) string {
	return seqRef(e.timelines[i], e.events[i])
}

func linkEvents(ctx context.Context, app App, cfg config.Config, in fieldValues) (linkChange, error) {
	refs, kind, err := readLinkRequest("event link", eventLinkDefinition(), in)
	if err != nil {
		return linkChange{}, err
	}
	id, err := app.NewID()
	if err != nil {
		return linkChange{}, err
	}
	author, err := eventAuthor(in.value("author"), cfg.Author, app.LookupEnv)
	if err != nil {
		return linkChange{}, err
	}
	dryRun, _ := in.boolean("dry-run")
	now := app.Now()

	change, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (linkChange, error) {
			change := linkChange{
				link:   timeline.Link{ID: id.String(), Kind: kind, CreatedAt: now, Author: author},
				dryRun: dryRun, verb: "link", done: "Linked", preposition: "to",
			}
			ends, err := findLinkEnds(ctx, tx, refs, timelineInputOf(in), app.LookupEnv)
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

func unlinkEvents(ctx context.Context, app App, cfg config.Config, in fieldValues) (linkChange, error) {
	refs, kind, err := readLinkRequest("event unlink", eventUnlinkDefinition(), in)
	if err != nil {
		return linkChange{}, err
	}
	dryRun, _ := in.boolean("dry-run")
	change, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (linkChange, error) {
			change := linkChange{dryRun: dryRun, verb: "unlink", done: "Unlinked", preposition: "from"}
			ends, err := findLinkEnds(ctx, tx, refs, timelineInputOf(in), app.LookupEnv)
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
func readLinkRequest(command string, def definition, in fieldValues) ([2]eventRef, timeline.LinkKind, error) {
	var refs [2]eventRef
	err := def.check(in, checkEnv{})
	if err != nil {
		return refs, "", err
	}
	values := in.values("events")
	if len(values) != len(refs) {
		return refs, "", missingValue(command, "two event refs, the event the link leaves and the one it reaches")
	}
	kind := in.value("kind")
	if kind == nil {
		return refs, "", missingValue(command, "--kind")
	}
	for i, value := range values {
		refs[i], err = parseEventRef(value)
		if err != nil {
			return refs, "", err
		}
	}

	return refs, timeline.LinkKind(*kind), nil
}

// findLinkEnds finds the events refs name.
func findLinkEnds(
	ctx context.Context, s records, refs [2]eventRef, timelines timelineInput, lookupEnv func(string) (string, bool),
) (linkEnds, error) {
	var ends linkEnds
	for i, ref := range refs {
		var err error
		ends.events[i], ends.timelines[i], err = findEvent(ctx, s, ref, timelines, lookupEnv)
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
	if p, ok := firstProblem(err); ok && p.Rule == timeline.RuleSelfLink {
		return &Error{
			Kind: kindOf(p.Rule), Message: fmt.Sprintf("`%s` cannot link to itself", ends.ref(0)), Hint: "Pass two different events.", Err: err,
		}
	}

	return recordError(err, "")
}

func linkExistsError(ends linkEnds, kind timeline.LinkKind, err error) *Error {
	return &Error{
		Kind: KindLinkExists, Message: fmt.Sprintf("`%s` already links to `%s` as %s", ends.ref(0), ends.ref(1), kind),
		Hint: fmt.Sprintf("Run 'coroner event view %s' to see its links.", ends.ref(0)), Err: err,
	}
}

func linkNotFoundError(ends linkEnds, kind timeline.LinkKind, err error) *Error {
	return &Error{
		Kind: KindLinkNotFound, Message: fmt.Sprintf("`%s` does not link to `%s` as %s", ends.ref(0), ends.ref(1), kind),
		Hint: fmt.Sprintf("Run 'coroner event view %s' to see its links.", ends.ref(0)), Err: err,
	}
}

func (c linkChange) render(o output) error {
	if o.format == formatJSON {
		err := o.json(newLinkJSON(c.link))
		if err != nil {
			return err
		}
	}

	did := c.done
	switch {
	case c.dryRun:
		did = "Would " + c.verb
	case c.created:
		err := o.printID(c.link.ID)
		if err != nil {
			return err
		}
	}

	return o.notify("%s `%s` %s `%s` as %s.", did, c.ends.ref(0), c.preposition, c.ends.ref(1), c.link.Kind)
}

func newLinkJSON(l timeline.Link) export.Link {
	return export.NewLink(l)
}
