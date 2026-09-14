package cli

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineExportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export --format {json | jsonl | md | mermaid | html} [-o <file> | -o <directory>/]",
		Short: "Export a timeline as JSON, JSONL, Markdown, Mermaid or HTML",
		Long: "Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its\n" +
			"events, their annotations, the sources they name and their attachments. --format json\n" +
			"writes one document, indented on a terminal; --format jsonl writes a header record, then\n" +
			"one record per line; --format md writes a CommonMark outline for people, with times in\n" +
			"UTC unless --tz names a zone.\n\n" +
			"--format mermaid writes a Mermaid gantt diagram of the events, one section per source, in\n" +
			"the same zone as md; --diagram appends that diagram to --format md.\n\n" +
			"--format html writes one self-contained page that shows the events on a timeline in a\n" +
			"browser, in the same zone as md, and needs -o.\n\n" +
			"The filter flags mean what they do on event list, and annotations on the events they\n" +
			"leave out are left out too. --order picks which end --limit keeps events from; exported\n" +
			"events are always in the order they happened. -o writes a file instead of stdout.\n\n" +
			"For md and jsonl, -o naming a directory, an existing one or a path ending in /, writes\n" +
			"<timeline>.md or <timeline>.jsonl into it, and each attachment's file beside it under\n" +
			"attachments/, named by its sha256, so the Markdown's images show wherever the directory\n" +
			"is committed or unzipped. Otherwise md links attachments to the files beside the\n" +
			"database, which only this machine has, and says so on stderr. Pasting Markdown into\n" +
			"GitHub, Linear or Slack uploads none of its images.\n\n" +
			"--group-by is experimental and may change before 1.0.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "json,jsonl,md,mermaid,html"},
		Example: "  $ coroner timeline export -t mongo-outage --format json > rca.json\n" +
			"  $ coroner timeline export --format jsonl --since \"2026-09-11T03:00Z\" --source mongod > mongod.jsonl\n" +
			"  $ coroner timeline export --format md --tz America/New_York > rca.md\n" +
			"  $ coroner timeline export --format mermaid --source mongod > timeline.mmd\n" +
			"  $ coroner timeline export --format md --diagram > rca.md\n" +
			"  $ coroner timeline export -t inc-4821 --format md -o rca/\n" +
			"  $ coroner timeline export --format html -o inc-4821.html",
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
	cmd.Flags().Bool("diagram", false, "Append a Mermaid gantt diagram of the events (--format md only)")
	cmd.Flags().StringP("output", "o", "-", "Write the export to this `file`, to stdout when it is -, or, for md and jsonl, into a directory with its attachments")

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

	return &Error{Kind: KindUsage, Message: "--format is required", Hint: "Pass --format json, --format jsonl, --format md, --format mermaid or --format html."}
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
	zone := exportZone(cmd.Flags(), z)
	path, err := outputPath(cmd.Flags(), o.format)
	if err != nil {
		return err
	}

	input := exportInput{version: app.Version, exportedAt: now, filter: filter}
	var page store.EventPage
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		input.timeline, err = selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		page, err = s.ListEvents(ctx, input.timeline.ID, filter)
		if err != nil {
			return err
		}
		input.events = page.Events
		input.annotations, err = s.Annotations(ctx, input.timeline.ID)
		if err != nil {
			return err
		}
		input.attachments, err = s.TimelineAttachments(ctx, input.timeline.ID)
		if err != nil {
			return err
		}
		input.sources, err = s.Sources(ctx)
		return err
	})
	if err != nil {
		return err
	}
	if bounds != "" {
		err = o.notify("Events %s", bounds)
		if err != nil {
			return err
		}
	}

	env := newExport(input)
	// Blobs are read only once the store is closed: withStorePath would report a permission
	// error reading one as the database's.
	dir := exportDirectory(path, o.format, app.Stat)
	switch {
	case dir != "":
		err = writeAttachmentFiles(o, app, dir, env.Attachments, cfg.DBPath)
		path = filepath.Join(dir, input.timeline.Name+"."+string(o.format))
	case o.format == formatMarkdown:
		markdown.AttachmentLink = func(a export.Attachment) string { return store.BlobPath(cfg.DBPath, a.SHA256) }
	}
	if err != nil {
		return err
	}
	err = o.writeTo(path, app.WriteFile, func(into output) error {
		return writeExport(into, env, markdown, zone)
	})
	if err == nil && dir == "" && o.format == formatMarkdown && len(env.Attachments) > 0 {
		err = o.notify("The Markdown links its %d attachments to files only this machine has; pass -o <directory>/ to write them beside it.",
			len(env.Attachments))
	}
	if err != nil {
		return err
	}
	if o.format == formatMermaid || markdown.Diagram {
		err = notifyLargeDiagram(o, env, zone)
		if err != nil {
			return err
		}
	}

	return notifyTruncated(o, page)
}

// notifyLargeDiagram warns when env's diagram is longer than Mermaid draws by default.
func notifyLargeDiagram(o output, env export.Envelope, zone *time.Location) error {
	size, err := export.MermaidSize(env, zone)
	if err != nil || size <= export.MermaidMaxTextSize {
		return err
	}

	return o.notify("The Mermaid diagram is %d characters, over the %d most hosts draw; narrow it with --since, --until or --source.",
		size, export.MermaidMaxTextSize)
}

// readMarkdownOptions returns how --format md presents the export.
func readMarkdownOptions(flags *pflag.FlagSet, chosen format, z zone) (export.MarkdownOptions, error) {
	if chosen != formatMarkdown {
		for _, name := range []string{"group-by", "include-raw", "diagram"} {
			if flags.Changed(name) {
				return export.MarkdownOptions{}, &Error{
					Kind: KindUsage, Message: "--" + name + " applies only to --format md", Hint: "Pass --format md, or leave --" + name + " out.",
				}
			}
		}

		return export.MarkdownOptions{}, nil
	}

	opts := export.MarkdownOptions{Zone: exportZone(flags, z)}
	opts.IncludeRaw, _ = flags.GetBool("include-raw")
	opts.Diagram, _ = flags.GetBool("diagram")
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

// exportZone returns the zone md, mermaid and html show times in: z when --tz was given, and
// otherwise UTC, so a shared document never depends on the zone of whoever exported it.
func exportZone(flags *pflag.FlagSet, z zone) *time.Location {
	if flags.Changed("tz") {
		return z.loc
	}

	return time.UTC
}

// outputPath returns the file -o names, - meaning stdout. --format html requires -o: writing
// the page is explicit (OUT-8).
func outputPath(flags *pflag.FlagSet, chosen format) (string, error) {
	path, _ := flags.GetString("output")
	switch {
	case chosen == formatHTML && !flags.Changed("output"):
		return "", &Error{Kind: KindUsage, Message: "--format html needs -o", Hint: "Pass -o inc-4821.html, or -o - to write the page to stdout."}
	case path == "":
		return "", &Error{Kind: KindUsage, Message: "-o must not be empty", Hint: "Pass -o <file>, or -o - for stdout."}
	}

	return path, nil
}

// exportDirectory returns the directory -o names for --format md or jsonl, which the export is
// written into beside its attachments: path when it ends in a separator or names a directory
// that exists. It returns "" for any other path, which is a file, or stdout.
func exportDirectory(path string, chosen format, stat func(string) (fs.FileInfo, error)) string {
	if chosen != formatMarkdown && chosen != formatJSONL || path == "-" {
		return ""
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator)) {
		return path
	}
	info, err := stat(path)
	if err == nil && info.IsDir() {
		return path
	}

	return ""
}

// writeAttachmentFiles creates dir and copies the content of each of attachments from its blob
// beside the database at dbPath to the file export.AttachmentFile names in dir, once per file,
// and says how many it wrote.
func writeAttachmentFiles(o output, app App, dir string, attachments []export.Attachment, dbPath string) error {
	cannotWrite := func(err error) error {
		return &Error{Kind: KindUsage, Message: "cannot write into " + dir, Err: err, Hint: "Pass -o a directory you can write to, or -o - for stdout."}
	}
	err := app.MkdirAll(dir, 0o755)
	if err != nil {
		return cannotWrite(err)
	}

	written := map[string]bool{}
	for _, a := range attachments {
		file := filepath.Join(dir, filepath.FromSlash(export.AttachmentFile(a)))
		if written[file] {
			continue
		}
		written[file] = true
		content, err := app.ReadFile(store.BlobPath(dbPath, a.SHA256))
		if err != nil {
			return fmt.Errorf("read the blob of attachment %s: %w", a.ID, err)
		}
		err = app.MkdirAll(filepath.Dir(file), 0o755)
		if err == nil {
			err = app.WriteFile(file, content, 0o644)
		}
		if err != nil {
			return cannotWrite(err)
		}
	}
	if len(written) == 0 {
		return nil
	}

	return o.notify("Wrote %d attachments to %s", len(written), filepath.Join(dir, "attachments"))
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
	// attachments are every attachment in the timeline.
	attachments []timeline.Attachment
	// sources are every source.
	sources []timeline.Source
}

// newExport returns input's timeline as an export: its events in ascending order, the
// annotations on the timeline or on those events, the attachments of those events and
// annotations, and the sources they name.
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
			exported[a.ID] = true
			if a.Source != nil {
				named[*a.Source] = true
			}
		}
	}
	for _, a := range input.attachments {
		if exported[*cmp.Or(a.Owner.EventID, a.Owner.AnnotationID)] {
			env.Attachments = append(env.Attachments, export.NewAttachment(a))
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
// jsonl as records, md through export.WriteMarkdown with the markdown options, and mermaid
// and html in zone.
func writeExport(o output, env export.Envelope, markdown export.MarkdownOptions, zone *time.Location) error {
	switch o.format {
	case formatMarkdown:
		return export.WriteMarkdown(o.out, env, markdown)
	case formatMermaid:
		return export.WriteMermaid(o.out, env, zone)
	case formatHTML:
		return export.WriteHTML(o.out, env, zone)
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

	// Unreachable: the formats annotation admits json, jsonl, md, mermaid and html only.
	return fmt.Errorf("timeline export cannot write --format %s", o.format)
}
