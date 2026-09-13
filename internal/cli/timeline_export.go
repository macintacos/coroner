package cli

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineExportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export --format {json|jsonl}",
		Short: "Export a timeline as JSON or JSONL",
		Long: "Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its\n" +
			"events, their annotations and the sources they name. --format json writes one document,\n" +
			"indented on a terminal; --format jsonl writes a header record, then one record per line.\n\n" +
			"The filter flags mean what they do on event list, and annotations on the events they\n" +
			"leave out are left out too. --order picks which end --limit keeps events from; exported\n" +
			"events are always in the order they happened. Redirect stdout to write a file.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "json,jsonl"},
		Example: "  $ coroner timeline export -t mongo-outage --format json > rca.json\n" +
			"  $ coroner timeline export --format jsonl --since \"2026-09-11T03:00Z\" --source mongod > mongod.jsonl",
		PreRunE: requireFormat,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return exportTimeline(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())
	addEventFilterFlags(cmd.Flags())

	return cmd
}

// requireFormat is usage unless --format or --json was given. It runs before wrapRunErrors
// checks the format, which would otherwise report the default text as unsupported.
func requireFormat(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()
	if asJSON, _ := flags.GetBool("json"); asJSON || flags.Changed("format") {
		return nil
	}

	return &Error{Kind: KindUsage, Message: "--format is required", Hint: "Pass --format json or --format jsonl."}
}

func exportTimeline(ctx context.Context, cmd *cobra.Command, app App) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}
	filter, bounds, err := readEventFilter(cmd.Flags(), app.Now(), z)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		in := exportInput{version: app.Version, at: app.Now(), filter: filter}
		in.timeline, err = selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		page, err := s.ListEvents(ctx, in.timeline.ID, filter)
		if err != nil {
			return err
		}
		in.events = page.Events
		in.annotations, err = s.Annotations(ctx, in.timeline.ID)
		if err != nil {
			return err
		}
		in.sources, err = s.Sources(ctx)
		if err != nil {
			return err
		}
		if bounds != "" {
			err = o.notify("Events %s", bounds)
			if err != nil {
				return err
			}
		}

		err = writeExport(o, newExport(in))
		if err != nil {
			return err
		}

		return notifyTruncated(o, page)
	})
}

// exportInput is what an export is made from, as the store returned it.
type exportInput struct {
	version  string
	at       time.Time
	timeline timeline.Timeline
	filter   store.EventFilter
	// events are in filter's order.
	events []timeline.Event
	// annotations are every annotation in the timeline.
	annotations []timeline.Annotation
	// sources are every source.
	sources []timeline.Source
}

// newExport returns in's timeline as an export: its events in ascending order, the
// annotations on the timeline or on those events, and the sources they name.
func newExport(in exportInput) export.Envelope {
	env := export.NewEnvelope(in.version, in.at)
	env.Query = newExportQuery(in.timeline.Name, in.filter)
	env.Timelines = []export.Timeline{export.NewTimeline(in.timeline)}

	events := slices.Clone(in.events)
	if in.filter.Order == store.OrderDesc {
		slices.Reverse(events)
	}
	exported, named := map[string]bool{}, map[string]bool{}
	for _, e := range events {
		env.Events = append(env.Events, export.NewEvent(e))
		exported[e.ID] = true
		if e.Source != nil {
			named[*e.Source] = true
		}
	}
	for _, a := range in.annotations {
		if a.EventID == nil || exported[*a.EventID] {
			env.Annotations = append(env.Annotations, export.NewAnnotation(a))
			if a.Source != nil {
				named[*a.Source] = true
			}
		}
	}
	for _, s := range in.sources {
		if named[s.Name] {
			env.Sources = append(env.Sources, export.NewSource(s))
		}
	}

	return env
}

func newExportQuery(timelineName string, f store.EventFilter) export.Query {
	q := export.Query{
		Timelines: []string{timelineName},
		Since:     (*export.Instant)(f.Since),
		Until:     (*export.Instant)(f.Until),
		Sources:   f.Sources,
		Tags:      f.Tags,
		Order:     "asc",
	}
	if f.Order == store.OrderDesc {
		q.Order = "desc"
	}
	if f.Contains != "" {
		q.Contains = &f.Contains
	}
	if f.Limit > 0 {
		q.Limit = &f.Limit
	}

	return q
}

// writeExport writes env as o's format asks: json as one document, indented on a terminal, and
// jsonl as records.
func writeExport(o output, env export.Envelope) error {
	switch o.format {
	case formatJSON:
		if o.outTTY {
			return export.Write(o.out, env, jsontext.WithIndent("  "))
		}

		return export.Write(o.out, env)
	case formatJSONL:
		records, err := export.Records(env)
		if err != nil {
			return err
		}

		return export.WriteRecords(o.out, records)
	case formatText:
	}

	// Unreachable: the formats annotation admits json and jsonl only.
	return nil
}
