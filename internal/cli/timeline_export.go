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
		Use:   "export --format {json | jsonl | md | mermaid | html | svg} [-o {<file> | <directory>/}]",
		Short: "Export a timeline as JSON, JSONL, Markdown, Mermaid, HTML or SVG",
		Long: "Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its\n" +
			"events, their annotations, the sources they name and their attachments. --format json\n" +
			"writes one document, indented on a terminal; --format jsonl writes a header record, then\n" +
			"one record per line; --format md writes a CommonMark outline for people, with times in\n" +
			"UTC unless --tz names a zone.\n\n" +
			"--format mermaid writes a Mermaid gantt diagram of the events, one section per source, in\n" +
			"the same zone as md; --diagram appends that diagram to --format md.\n\n" +
			"--format html writes one self-contained page that shows the events on a timeline in a\n" +
			"browser, in the same zone as md, and needs -o.\n\n" +
			"--format svg writes a static SVG picture of the events, one lane per source, in the same\n" +
			"zone as md, for documents and slides that draw neither Mermaid nor HTML.\n\n" +
			"The filter flags mean what they do on event list, and annotations on the events they\n" +
			"leave out are left out too. --order picks which end --limit keeps events from; exported\n" +
			"events are always in the order they happened.\n\n" +
			"-o writes a file instead of stdout. For md and jsonl, -o may instead name a directory, one\n" +
			"that exists or a path ending in /: coroner writes <timeline>.md or <timeline>.jsonl into\n" +
			"it and each attachment's file under attachments/ beside it, named by its sha256, so the\n" +
			"Markdown's images show wherever the directory is committed or unzipped. Otherwise md links\n" +
			"attachments to the files beside the database, which only this machine has, and says so on\n" +
			"stderr, and a jsonl export's attachment records carry metadata and sha256 only, so their\n" +
			"files stay on this machine. Pasting Markdown into GitHub, Linear or Slack uploads none of\n" +
			"its images.\n\n" +
			"Repeat -t to overlay several timelines in one json, jsonl, md, mermaid or html export: it\n" +
			"holds every timeline, and their events merge by occurred at, then timeline id, then seq. md\n" +
			"labels each event with its timeline, mermaid draws a section per timeline and source, as in\n" +
			"mongo-failover/mongod, and html nests each timeline's sources under a row for the timeline,\n" +
			"with a checkbox to filter by it. svg exports one timeline. An overlay written into a directory\n" +
			"is named after its timelines joined by +, as in mongo-failover+checkout-latency.jsonl.\n" +
			"--overlay <name> exports the timelines an overlay saved with 'coroner overlay create' holds,\n" +
			"in place of -t. json and jsonl carry every link between two exported events.\n\n" +
			"--group-by is experimental and may change before 1.0.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "json,jsonl,md,mermaid,html,svg", overlayAnnotation: "true"},
		Example: "  $ coroner timeline export -t mongo-outage --format json > rca.json\n" +
			"  $ coroner timeline export --format jsonl --since \"2026-09-11T03:00Z\" --source mongod > mongod.jsonl\n" +
			"  $ coroner timeline export --format md --tz America/New_York > rca.md\n" +
			"  $ coroner timeline export --format mermaid --source mongod > timeline.mmd\n" +
			"  $ coroner timeline export --format md --diagram > rca.md\n" +
			"  $ coroner timeline export -t inc-4821 --format md -o rca/\n" +
			"  $ coroner timeline export --format html -o inc-4821.html\n" +
			"  $ coroner timeline export -t inc-4821 --format svg > inc-4821.svg\n" +
			"  $ coroner timeline export -t mongo-failover -t checkout-latency --format jsonl > overlay.jsonl\n" +
			"  $ coroner timeline export --overlay checkout-incident --format md -o rca/",
		PreRunE: requireFormat,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return exportTimeline(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())
	cmd.Flags().Lookup("timeline").Usage = "Timeline `name|id` to export; repeat to overlay several (json, jsonl, md, mermaid and html only)"
	addOverlayFlag(cmd.Flags())
	addEventFilterFlags(cmd.Flags())
	cmd.Flags().Lookup("order").Usage = "Keep the earliest or latest events when --limit cuts the export, as an `order`: {asc|desc}"
	cmd.Flags().Lookup("limit").Usage = "Export at most this `number` of events (default: every one)"
	cmd.Flags().String("group-by", "", "Section Markdown events by a `field`: {source|tag} (experimental)")
	cmd.Flags().Bool("include-raw", false, "Add each event's time as it was given (--format md only)")
	cmd.Flags().Bool("diagram", false, "Append a Mermaid gantt diagram of the events (--format md only)")
	cmd.Flags().StringP("output", "o", "-", "Write the export to this `path`, to stdout when it is -, or, for md and jsonl, into a directory with its attachments")

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

	return &Error{Kind: KindUsage, Message: "--format is required", Hint: "Pass --format json, --format jsonl, --format md, --format mermaid, --format html or --format svg."}
}

func exportTimeline(ctx context.Context, cmd *cobra.Command, app App) error {
	choice, err := readTimelineChoice(cmd.Flags())
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

	var input exportInput
	var page store.EventPage
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		input, page, err = readExport(ctx, s, choice, filter, app.LookupEnv)
		return err
	})
	if err == nil {
		err = checkOverlayFormat(len(input.timelines), o.format)
	}
	if err != nil {
		return err
	}
	input.version, input.exportedAt = app.Version, now
	if bounds != "" {
		err = o.notify("Events %s", bounds)
		if err != nil {
			return err
		}
	}

	env := newExport(input)
	dir := exportDirectory(path, o.format, app.Stat)
	var copies []fileCopy
	if dir != "" {
		copies = attachmentCopies(dir, cfg.DBPath, env.Attachments)
		err = copyFiles(fileSystem{readFile: app.ReadFile, writeFile: app.WriteFile, mkdirAll: app.MkdirAll}, dir, copies)
		if err != nil {
			return err
		}
		// ponytail: NAME_MAX caps an overlay's file name at about four long timeline names; hash or truncate when that bites.
		path = filepath.Join(dir, strings.Join(namesOf(input.timelines), "+")+"."+string(o.format))
	}
	linksLocal := dir == "" && o.format == formatMarkdown && len(env.Attachments) > 0
	if linksLocal {
		markdown.AttachmentLink = func(a export.Attachment) string { return blobLink(path, store.BlobPath(cfg.DBPath, a.SHA256)) }
	}
	err = o.writeTo(path, app.WriteFile, func(into output) error {
		return writeExport(into, env, markdown, zone)
	})
	if err != nil {
		return err
	}
	switch {
	case linksLocal:
		err = o.notify("The Markdown links its attachments to files only this machine has; pass -o <directory>/ to write them beside it.")
	case len(copies) > 0:
		err = notifyCopied(o, copies)
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

// checkOverlayFormat returns usage when -t selects count timelines, more than one, for a format
// that exports one.
func checkOverlayFormat(count int, chosen format) error {
	switch chosen {
	case formatJSON, formatJSONL, formatMarkdown, formatMermaid, formatHTML:
		return nil
	case formatSVG, formatText:
	}
	if count <= 1 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("--format %s exports one timeline, and -t selects %d", chosen, count),
		Hint:    "Pass -t once, or overlay the timelines with --format json, jsonl, md, mermaid or html.",
	}
}

// readExport reads what an export is made from: the timelines choice selects, the events filter
// keeps in them and their page, every annotation, attachment and link from an event in those
// timelines, and every source.
func readExport(
	ctx context.Context, s *store.Store, choice timelineChoice, filter store.EventFilter, lookupEnv func(string) (string, bool),
) (exportInput, store.EventPage, error) {
	timelines, err := selectChosenTimelines(ctx, s, choice, lookupEnv)
	if err != nil {
		return exportInput{}, store.EventPage{}, err
	}
	filter.TimelineIDs = timelineIDs(timelines)
	input := exportInput{timelines: timelines, filter: filter}
	page, err := s.ListEvents(ctx, filter)
	if err != nil {
		return input, page, err
	}
	input.events = page.Events
	input.annotations, err = readEach(timelines, func(id string) ([]timeline.Annotation, error) { return s.Annotations(ctx, id) })
	if err != nil {
		return input, page, err
	}
	input.attachments, err = readEach(timelines, func(id string) ([]timeline.Attachment, error) { return s.TimelineAttachments(ctx, id) })
	if err != nil {
		return input, page, err
	}
	input.links, err = readEach(timelines, func(id string) ([]timeline.Link, error) { return s.TimelineLinks(ctx, id) })
	if err != nil {
		return input, page, err
	}
	input.sources, err = s.Sources(ctx)

	return input, page, err
}

func namesOf(timelines []timeline.Timeline) []string {
	names := make([]string, 0, len(timelines))
	for _, t := range timelines {
		names = append(names, t.Name)
	}

	return names
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

// exportZone returns the zone md, mermaid, html and svg show times in: z when --tz was given, and
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
	if (chosen != formatMarkdown && chosen != formatJSONL) || path == "-" {
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

// fileCopy is one attachment's file in an export directory and the blob its content comes from.
type fileCopy struct {
	attachmentID, from, to string
}

// attachmentCopies returns, once per destination file, the blob beside the database at
// dbPath that each attachment's file in dir is copied from.
func attachmentCopies(dir, dbPath string, attachments []export.Attachment) []fileCopy {
	var copies []fileCopy
	planned := map[string]bool{}
	for _, a := range attachments {
		to := filepath.Join(dir, filepath.FromSlash(export.AttachmentPath(a)))
		if !planned[to] {
			planned[to] = true
			copies = append(copies, fileCopy{attachmentID: a.ID, from: store.BlobPath(dbPath, a.SHA256), to: to})
		}
	}

	return copies
}

// fileSystem is the file access copyFiles needs, as App provides it.
type fileSystem struct {
	readFile  func(name string) ([]byte, error)
	writeFile func(name string, data []byte, perm fs.FileMode) error
	mkdirAll  func(path string, perm fs.FileMode) error
}

// copyFiles creates dir, then each copy's directory, and writes the source's bytes to it.
// Call it only once the store is closed: withStorePath would report a permission error
// reading a blob as the database's.
func copyFiles(files fileSystem, dir string, copies []fileCopy) error {
	cannotWrite := func(err error) error {
		return &Error{Kind: KindUsage, Message: "cannot write into " + dir, Err: err, Hint: "Pass -o a directory you can write to, or -o - for stdout."}
	}
	err := files.mkdirAll(dir, 0o755)
	if err != nil {
		return cannotWrite(err)
	}
	for _, c := range copies {
		content, err := files.readFile(c.from)
		if err != nil {
			return fmt.Errorf("read the blob of attachment %s: %w", c.attachmentID, err)
		}
		err = files.mkdirAll(filepath.Dir(c.to), 0o755)
		if err == nil {
			err = files.writeFile(c.to, content, 0o644)
		}
		if err != nil {
			return cannotWrite(err)
		}
	}

	return nil
}

// notifyCopied says how many attachment files copies wrote, and where.
func notifyCopied(o output, copies []fileCopy) error {
	noun := "files"
	if len(copies) == 1 {
		noun = "file"
	}

	return o.notify("Wrote %d attachment %s to %s", len(copies), noun, filepath.Dir(copies[0].to))
}

// blobLink returns where Markdown written to path links the blob at blob: relative to path's
// directory when both are relative, so the link resolves from the file. On stdout, when
// either path is absolute, or when only the working directory could relate them, it is blob.
func blobLink(path, blob string) string {
	if path == "-" || filepath.IsAbs(path) || filepath.IsAbs(blob) {
		return blob
	}
	rel, err := filepath.Rel(filepath.Dir(path), blob)
	if err != nil {
		return blob
	}

	return rel
}

// exportInput is what an export is made from, as the store returned it.
type exportInput struct {
	version    string
	exportedAt time.Time
	timelines  []timeline.Timeline
	filter     store.EventFilter
	// events are in filter's order.
	events []timeline.Event
	// annotations are every annotation in the timelines.
	annotations []timeline.Annotation
	// attachments are every attachment in the timelines.
	attachments []timeline.Attachment
	// links are every link from an event in the timelines.
	links []timeline.Link
	// sources are every source.
	sources []timeline.Source
}

// newExport returns input's timelines as an export: their events in ascending order, the
// annotations on a timeline or on those events, the attachments of those events and
// annotations, the links between two of those events, and the sources they name.
func newExport(input exportInput) export.Envelope {
	env := export.NewEnvelope(input.version, input.exportedAt)
	env.Query = newExportQuery(input.timelines, input.filter)
	for _, t := range input.timelines {
		env.Timelines = append(env.Timelines, export.NewTimeline(t))
	}

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
	for _, l := range input.links {
		if exported[l.FromEventID] && exported[l.ToEventID] {
			env.Links = append(env.Links, export.NewLink(l))
		}
	}
	for _, s := range input.sources {
		if named[s.Name] {
			env.Sources = append(env.Sources, export.NewSource(s))
		}
	}

	return env
}

func newExportQuery(timelines []timeline.Timeline, f store.EventFilter) export.Query {
	q := export.Query{
		Timelines: namesOf(timelines),
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
// jsonl as records, md through export.WriteMarkdown with the markdown options, and mermaid,
// html and svg in zone.
func writeExport(o output, env export.Envelope, markdown export.MarkdownOptions, zone *time.Location) error {
	switch o.format {
	case formatMarkdown:
		return export.WriteMarkdown(o.out, env, markdown)
	case formatMermaid:
		return export.WriteMermaid(o.out, env, zone)
	case formatHTML:
		return export.WriteHTML(o.out, env, zone)
	case formatSVG:
		return export.WriteSVG(o.out, env, zone)
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

	// Unreachable: the formats annotation admits json, jsonl, md, mermaid, html and svg only.
	return fmt.Errorf("timeline export cannot write --format %s", o.format)
}
