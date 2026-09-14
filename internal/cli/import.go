package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newImportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "import {<file> | -}",
		Short:   "Merge a JSONL export into the database",
		GroupID: coreCommands,
		Long: "Merge a coroner JSONL export, as 'coroner timeline export --format jsonl' writes it, into the\n" +
			"database: its timelines, sources, events, annotations and attachments. - reads the export\n" +
			"from stdin, so concatenated exports import together. Every record is checked before\n" +
			"anything is written, and the import is written in one transaction.\n\n" +
			"Records match by id, and sources by name. A record stored unchanged is skipped, so importing\n" +
			"a file twice changes nothing. A record stored with different content is a conflict: by\n" +
			"default the import writes nothing and exits 4; --on-conflict skip keeps the stored record,\n" +
			"and --on-conflict replace takes the import's. Authors and created and updated times come\n" +
			"from the export. An imported event keeps its seq unless its timeline has already used it.\n\n" +
			"An attachment is imported with its file from the attachments/ directory beside the export,\n" +
			"which 'coroner timeline export -o <directory>/' writes, or when the database already has\n" +
			"that file. Otherwise it is skipped and listed.\n\n" +
			"-t names a stored timeline that every record must belong to. Without -t, the export decides\n" +
			"which timelines it imports into.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner import colleague-annotations.jsonl --dry-run --json\n" +
			"  $ coroner import colleague-annotations.jsonl\n" +
			"  $ cat a.jsonl b.jsonl | coroner import -\n" +
			"  $ coroner import inc-4821.jsonl --on-conflict skip",
		RunE: func(cmd *cobra.Command, args []string) error {
			return importExport(ctx, cmd, app, args[0])
		},
	}

	addTimelineFlag(cmd.Flags())
	cmd.Flags().String("on-conflict", string(onConflictFail), "What to do with a record stored with different content, as a `policy`: {fail|skip|replace}")
	cmd.Flags().Bool("dry-run", false, "Show what the import would change, and write nothing")

	return cmd
}

func importExport(ctx context.Context, cmd *cobra.Command, app App, path string) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	policy, err := readConflictPolicy(cmd.Flags())
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}
	records, err := readImportRecords(path, app)
	if err != nil {
		return err
	}
	// Read before the store opens: withStorePath would report a permission error on either as the
	// database's.
	sidecars, err := readSidecars(path, records, app.ReadFile)
	if err != nil {
		return err
	}

	in := importInput{
		records: records, sidecars: sidecars, localBlobs: localBlobs(cfg.DBPath, records, app.Stat),
		onConflict: policy, newID: app.NewID, now: app.Now(),
	}
	var plan importPlan
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		plan, err = planStoredImport(ctx, s, in, flag, app.LookupEnv)
		if err != nil || dryRun || plan.blocked != nil {
			return err
		}

		return s.Import(ctx, plan.write)
	})
	if err != nil {
		return err
	}

	plan.result.DryRun = dryRun
	err = renderImport(o, plan.result, dryRun || plan.blocked != nil)
	if err != nil || plan.blocked == nil {
		return err
	}

	return plan.blocked
}

// planStoredImport plans in against what s holds, within the timeline flag selects when it is
// set.
// ponytail: the plan reads outside the write transaction, so a writer in between surfaces as a
// constraint error rather than a conflict; plan inside Store.write if concurrent imports matter.
func planStoredImport(
	ctx context.Context, s *store.Store, in importInput, flag *string, lookupEnv func(string) (string, bool),
) (importPlan, error) {
	if flag != nil {
		guard, _, err := lookupTimeline(ctx, s, flag, lookupEnv)
		if err != nil {
			return importPlan{}, err
		}
		in.guard = &guard
	}

	var err error
	in.stored, err = readStoredRecords(ctx, s, in.records)
	if err != nil {
		return importPlan{}, err
	}

	return planImport(in)
}

// readConflictPolicy returns the policy --on-conflict names.
func readConflictPolicy(flags *pflag.FlagSet) (conflictPolicy, error) {
	value, _ := flags.GetString("on-conflict")
	policy := conflictPolicy(value)
	switch policy {
	case onConflictFail, onConflictSkip, onConflictReplace:
		return policy, nil
	}

	return "", &Error{
		Kind: KindUsage, Message: fmt.Sprintf("--on-conflict %q is not fail, skip or replace", value),
		Hint: "Pass --on-conflict fail, --on-conflict skip or --on-conflict replace.",
	}
}

// readImportRecords reads and decodes the export at path, or on stdin when path is -. It never
// reads a terminal.
// ponytail: the whole export is read into memory; stream it through export.Decode if exports
// grow past what fits.
func readImportRecords(path string, app App) ([]export.Record, error) {
	var data []byte
	var err error
	switch {
	case path == "-" && app.InIsTTY:
		return nil, &Error{
			Kind: KindUsage, Message: "- reads the export from stdin, which is a terminal",
			Hint: "Pipe an export in, as in: cat inc-4821.jsonl | coroner import -",
		}
	case path == "-":
		data, err = io.ReadAll(app.In)
		if err != nil {
			return nil, fmt.Errorf("read the export from stdin: %w", err)
		}
	default:
		data, err = app.ReadFile(path)
		if err != nil {
			return nil, &Error{
				Kind: KindUsage, Message: "cannot read " + path, Err: err,
				Hint: "Pass an export file that exists and you can read, or - to read one from stdin.",
			}
		}
	}

	records, err := export.Decode(bytes.NewReader(data))
	if invalid, ok := errors.AsType[*export.DecodeError](err); ok {
		return nil, &Error{Kind: KindInvalidInput, Message: invalid.Error(), Hint: invalidImportHint, Err: err}
	}

	return records, err
}

// readSidecars returns the content of each attachment's file in the attachments/ directory
// beside the export at path, by attachment id, leaving out files that are not there. Stdin has
// no directory beside it.
func readSidecars(path string, records []export.Record, readFile func(string) ([]byte, error)) (map[string][]byte, error) {
	sidecars := map[string][]byte{}
	if path == "-" {
		return sidecars, nil
	}

	for _, r := range records {
		a, ok := r.(export.Attachment)
		if !ok {
			continue
		}
		file := filepath.Join(filepath.Dir(path), filepath.FromSlash(export.AttachmentPath(a)))
		content, err := readFile(file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, &Error{Kind: KindUsage, Message: "cannot read " + file, Hint: "Check that file's permissions, then import again.", Err: err}
		default:
			sidecars[a.ID] = content
		}
	}

	return sidecars, nil
}

// localBlobs returns the sha256 of each attachment in records whose blob is beside the database
// at dbPath.
func localBlobs(dbPath string, records []export.Record, stat func(string) (fs.FileInfo, error)) map[string]bool {
	blobs := map[string]bool{}
	for _, r := range records {
		if a, ok := r.(export.Attachment); ok {
			_, err := stat(store.BlobPath(dbPath, a.SHA256))
			blobs[a.SHA256] = err == nil
		}
	}

	return blobs
}

// readStoredRecords returns every stored timeline and source, and the stored events, annotations
// and attachments whose ids records name.
// ponytail: one read per id; a batched read if large imports read slowly.
func readStoredRecords(ctx context.Context, s *store.Store, records []export.Record) (storedRecords, error) {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return storedRecords{}, err
	}
	stored := storedRecords{
		timelines: map[string]timeline.Timeline{}, events: map[string]timeline.Event{},
		annotations: map[string]timeline.Annotation{}, attachments: map[string]timeline.Attachment{},
	}
	for _, t := range timelines {
		stored.timelines[t.ID] = t
	}
	stored.sources, err = s.Sources(ctx)
	if err != nil {
		return storedRecords{}, err
	}

	attachment := func(ctx context.Context, id string) (timeline.Attachment, error) {
		found, err := s.AttachmentsByIDPrefix(ctx, id, 1)
		if err == nil && len(found) == 0 {
			err = store.ErrNotFound
		}
		if err != nil {
			return timeline.Attachment{}, err
		}

		return found[0], nil
	}
	for _, r := range records {
		switch r := r.(type) {
		case export.Event:
			err = addStored(ctx, &r.ID, stored.events, s.Event)
		case export.Annotation:
			err = errors.Join(addStored(ctx, &r.ID, stored.annotations, s.Annotation), addStored(ctx, r.EventID, stored.events, s.Event))
		case export.Attachment:
			err = errors.Join(
				addStored(ctx, &r.ID, stored.attachments, attachment),
				addStored(ctx, r.EventID, stored.events, s.Event), addStored(ctx, r.AnnotationID, stored.annotations, s.Annotation),
			)
		}
		if err != nil {
			return storedRecords{}, err
		}
	}

	return stored, nil
}

// addStored adds the record get finds under id to into, unless id is nil, already there or not
// stored.
func addStored[T any](ctx context.Context, id *string, into map[string]T, get func(context.Context, string) (T, error)) error {
	if id == nil {
		return nil
	}
	if _, ok := into[*id]; ok {
		return nil
	}

	record, err := get(ctx, *id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	into[*id] = record

	return nil
}

// renderImport says on stderr what the import did, or would do when unwritten, and prints the
// result object under --json.
func renderImport(o output, r importResult, unwritten bool) error {
	verb := "Imported"
	if unwritten {
		verb = "Would import"
	}
	lines := make([]string, 0, 6+len(r.Conflicts))
	for _, c := range []struct {
		name   string
		counts importCounts
	}{
		{"timelines", r.Timelines}, {"sources", r.Sources}, {"events", r.Events}, {"annotations", r.Annotations}, {"attachments", r.Attachments},
	} {
		lines = append(lines, fmt.Sprintf("%s %s: %d inserted, %d identical, %d conflicting",
			verb, c.name, c.counts.Inserted, c.counts.Identical, c.counts.Conflicting))
	}
	for _, c := range r.Conflicts {
		lines = append(lines, fmt.Sprintf("Conflict: %s %s differs in %s", c.Type, c.ID, strings.Join(c.Fields, ", ")))
	}
	if n := len(r.MissingAttachments); n > 0 {
		noun := "attachments"
		if n == 1 {
			noun = "attachment"
		}
		lines = append(lines, fmt.Sprintf(
			"Skipped %d %s whose files are neither beside the export nor in the database; import an export written with -o <directory>/ to bring them",
			n, noun))
	}

	for _, line := range lines {
		err := o.notify("%s", line)
		if err != nil {
			return err
		}
	}
	if o.format == formatJSON {
		return o.json(r)
	}

	return nil
}
