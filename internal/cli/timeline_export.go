package cli

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineExportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export --format {json | jsonl | md}",
		Short: "Export a timeline as JSON, JSONL or Markdown",
		Long: "Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its\n" +
			"events, their annotations and the sources they name. --format json writes one document,\n" +
			"indented on a terminal; --format jsonl writes a header record, then one record per line;\n" +
			"--format md writes a CommonMark outline for people, with times in UTC unless --tz names\n" +
			"a zone.\n\n" +
			"The filter flags mean what they do on event list, and annotations on the events they\n" +
			"leave out are left out too. --order picks which end --limit keeps events from; exported\n" +
			"events are always in the order they happened. Redirect stdout to write a file.\n\n" +
			"--group-by is experimental and may change before 1.0.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "json,jsonl,md"},
		Example: "  $ coroner timeline export -t mongo-outage --format json > rca.json\n" +
			"  $ coroner timeline export --format jsonl --since \"2026-09-11T03:00Z\" --source mongod > mongod.jsonl\n" +
			"  $ coroner timeline export --format md --tz America/New_York > rca.md",
		PreRunE: requireFormat,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return exportTimeline(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())
	addEventFilterFlags(cmd.Flags())
	cmd.Flags().Lookup("order").Usage = "Keep the earliest or latest events when --limit cuts the export, as an `order`: {asc|desc}"
	cmd.Flags().Lookup("limit").Usage = "Export at most this `number` of events (default: every one)"
	cmd.Flags().String("group-by", "", "Section Markdown events by a `field`: {source|tag} (experimental)")
	cmd.Flags().Bool("include-raw", false, "Add each event's time as it was given (--format md only)")

	return cmd
}

// requireFormat returns a usage error unless --format or --json was given. It is a PreRunE
// because wrapRunErrors checks the format before RunE, and would report the default text as
// unsupported.
func requireFormat(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()
	if asJSON, _ := flags.GetBool("json"); asJSON || flags.Changed("format") {
		return nil
	}

	return &Error{Kind: KindUsage, Message: "--format is required", Hint: "Pass --format json, --format jsonl or --format md."}
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
	now := app.Now()
	filter, bounds, err := readEventFilter(cmd.Flags(), now, z)
	if err != nil {
		return err
	}
	markdown, err := readMarkdownOptions(cmd.Flags(), o.format, z)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		input := exportInput{version: app.Version, exportedAt: now, filter: filter}
		input.timeline, err = selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		page, err := s.ListEvents(ctx, input.timeline.ID, filter)
		if err != nil {
			return err
		}
		input.events = page.Events
		input.annotations, err = s.Annotations(ctx, input.timeline.ID)
		if err != nil {
			return err
		}
		input.sources, err = s.Sources(ctx)
		if err != nil {
			return err
		}
		if bounds != "" {
			err = o.notify("Events %s", bounds)
			if err != nil {
				return err
			}
		}

		err = writeExport(o, newExport(input), markdown)
		if err != nil {
			return err
		}

		return notifyTruncated(o, page)
	})
}

// readMarkdownOptions returns how --format md presents the export: in z when --tz was given,
// and otherwise in UTC, so a shared document never depends on the zone of whoever exported it.
func readMarkdownOptions(flags *pflag.FlagSet, chosen format, z zone) (export.MarkdownOptions, error) {
	if chosen != formatMarkdown {
		for _, name := range []string{"group-by", "include-raw"} {
			if flags.Changed(name) {
				return export.MarkdownOptions{}, &Error{
					Kind: KindUsage, Message: "--" + name + " applies only to --format md", Hint: "Pass --format md, or leave --" + name + " out.",
				}
			}
		}

		return export.MarkdownOptions{}, nil
	}

	opts := export.MarkdownOptions{Zone: time.UTC}
	if flags.Changed("tz") {
		opts.Zone = z.loc
	}
	opts.IncludeRaw, _ = flags.GetBool("include-raw")
	if groupBy := changedString(flags, "group-by"); groupBy != nil {
		var ok bool
		opts.GroupBy, ok = export.ParseGroupBy(*groupBy)
		// ParseGroupBy reads "" as the default, but A-9 rejects an empty flag value.
		if !ok || *groupBy == "" {
			return opts, &Error{
				Kind: KindUsage, Message: fmt.Sprintf("--group-by %q is not source or tag", *groupBy), Hint: "Pass --group-by source or --group-by tag.",
			}
		}
	}

	return opts, nil
}

// exportInput is what an export is made from, as the store returned it.
type exportInput struct {
	version    string
	exportedAt time.Time
	timeline   timeline.Timeline
	filter     store.EventFilter
	// events are in filter's order.
	events []timeline.Event
	// annotations are every annotation in the timeline.
	annotations []timeline.Annotation
	// sources are every source.
	sources []timeline.Source
}

// newExport returns input's timeline as an export: its events in ascending order, the
// annotations on the timeline or on those events, and the sources they name.
func newExport(input exportInput) export.Envelope {
	env := export.NewEnvelope(input.version, input.exportedAt)
	env.Query = newExportQuery(input.timeline.Name, input.filter)
	env.Timelines = []export.Timeline{export.NewTimeline(input.timeline)}

	events := slices.Clone(input.events)
	if input.filter.Order == store.OrderDesc {
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
	for _, a := range input.annotations {
		if a.EventID == nil || exported[*a.EventID] {
			env.Annotations = append(env.Annotations, export.NewAnnotation(a))
			if a.Source != nil {
				named[*a.Source] = true
			}
		}
	}
	for _, s := range input.sources {
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

// writeExport writes env as o's format asks: json as one document, indented on a terminal,
// jsonl as records, and md through export.WriteMarkdown with the markdown options.
func writeExport(o output, env export.Envelope, markdown export.MarkdownOptions) error {
	switch o.format {
	case formatMarkdown:
		return export.WriteMarkdown(o.out, env, markdown)
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

	// Unreachable: the formats annotation admits json, jsonl and md only.
	return fmt.Errorf("timeline export cannot write --format %s", o.format)
}
