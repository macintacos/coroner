package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventDeleteCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <event>...",
		Short: "Delete events and their annotations",
		Long: "Delete events, with their tags and annotations, and print each as it was, so its fields\n" +
			"can be added again. Every event is found before any is deleted: when one does not exist,\n" +
			"nothing is deleted and delete exits 3. Exit 3 means one of the events does not exist: if\n" +
			"a retry exits 3, the first attempt may already have deleted them. A deleted event's seq\n" +
			"is never given to another event.\n\n" + eventRefHelp,
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event delete 12 14 --dry-run\n" +
			"  $ coroner event delete mongo-outage-2026-09-11#12 --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteEvents(ctx, cmd, app, args)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be deleted, and delete nothing")

	return cmd
}

func deleteEvents(ctx context.Context, cmd *cobra.Command, app App, refs []string) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	parsed := make([]eventRef, 0, len(refs))
	for _, ref := range refs {
		p, err := parseEventRef(ref)
		if err != nil {
			return err
		}
		parsed = append(parsed, p)
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		found, err := findEvents(ctx, s, parsed, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		if dryRun {
			err = writeDeletion(o, z, found)
			if err != nil {
				return err
			}

			return notifyDeletion(o, "Would delete", found)
		}

		err = s.DeleteEvents(ctx, found.ids()...)
		if errors.Is(err, store.ErrNotFound) {
			return eventNotFoundError("another coroner deleted one of the events first; nothing was deleted", err)
		}
		if err != nil {
			return err
		}
		err = writeDeletion(o, z, found)
		if err != nil {
			return err
		}

		return notifyDeletion(o, "Deleted", found)
	})
}

// foundEvents are events with their timelines, each once, in the order first named.
type foundEvents struct {
	events    []timeline.Event
	timelines map[string]timeline.Timeline
}

// findEvents finds the event each of refs names, failing on the first that finds none.
func findEvents(ctx context.Context, s *store.Store, refs []eventRef, selection timelineSelection) (foundEvents, error) {
	found := foundEvents{timelines: map[string]timeline.Timeline{}}
	for _, ref := range refs {
		e, t, err := findEvent(ctx, s, ref, selection)
		if err != nil {
			return found, err
		}
		if !slices.ContainsFunc(found.events, func(named timeline.Event) bool { return named.ID == e.ID }) {
			found.events = append(found.events, e)
			found.timelines[t.ID] = t
		}
	}

	return found, nil
}

func (f foundEvents) ids() []string {
	ids := make([]string, 0, len(f.events))
	for _, e := range f.events {
		ids = append(ids, e.ID)
	}

	return ids
}

// writeDeletion prints the events f holds as they were: the event list's JSON, or a summary
// of each.
func writeDeletion(o output, z zone, f foundEvents) error {
	if o.format == formatJSON {
		return o.json(newEventListJSON(store.EventPage{Events: f.events, Total: len(f.events)}))
	}

	summaries := make([]string, 0, len(f.events))
	for _, e := range f.events {
		summaries = append(summaries, eventSummary(e, f.timelines[e.TimelineID], z))
	}

	_, err := io.WriteString(o.out, strings.Join(summaries, "\n"))
	return err
}

// notifyDeletion says, per timeline, how many of f's events verb applies to.
func notifyDeletion(o output, verb string, f foundEvents) error {
	counts := map[string]int{}
	var order []string
	for _, e := range f.events {
		if counts[e.TimelineID] == 0 {
			order = append(order, e.TimelineID)
		}
		counts[e.TimelineID]++
	}

	for _, id := range order {
		noun := "events"
		if counts[id] == 1 {
			noun = "event"
		}
		err := o.notify("%s %d %s from timeline `%s`.", verb, counts[id], noun, f.timelines[id].Name)
		if err != nil {
			return err
		}
	}

	return nil
}
