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
		Short:   "Import records from a JSONL export",
		GroupID: coreCommands,
		Long: "Import the records of a coroner JSONL export, as 'coroner timeline export --format jsonl'\n" +
			"writes it, into the database: its timelines, sources, events, annotations and attachments.\n" +
			"- reads the export from stdin, so concatenated exports import together. Every record is\n" +
			"checked before anything is written, and the import is written in one transaction.\n\n" +
			"Records match by id, and sources by name. A record stored unchanged is skipped, so importing a\n" +
			"file twice changes nothing. A record stored with different content is a conflict: by default\n" +
			"the import writes nothing and exits 4; --on-conflict skip keeps the stored record, and\n" +
			"--on-conflict replace takes the import's. Replace never changes an attachment or moves an event\n" +
			"or annotation, so such a conflict exits 4 under replace too. Authors and created and updated\n" +
			"times come from the export. An imported event keeps its seq unless its timeline has already\n" +
			"reached that seq, when it gets the next one.\n\n" +
			"An attachment is imported with its file from the attachments/ directory beside the export,\n" +
			"which 'coroner timeline export -o <directory>/' writes, or when the database already has that\n" +
			"file. Otherwise it is left out and listed.\n\n" +
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
	records, err := readImportRecords(path, app.ReadFile, app.In, app.InIsTTY)
	if err != nil {
		return err
	}
	// Read before the store opens: withStoreOpenedBy would report a permission error on either as
	// the database's.
	sidecars, err := readSidecars(path, records, app.ReadDir, app.ReadFile)
	if err != nil {
		return err
	}

	in := importInput{
		records: records, sidecars: sidecars, localBlobs: localBlobs(cfg.DBPath, records, app.Stat),
		onConflict: policy, newID: app.NewID, now: app.Now(),
	}
	open := app.OpenStore
	if dryRun {
		open = app.OpenStoreReadOnly
	}
	var plan importPlan
	err = withStoreOpenedBy(ctx, open, cfg.DBPath, func(s *store.Store) error {
		plan, err = planStoredImport(ctx, s, in, flag)
		if err != nil || dryRun || plan.blocked != nil {
			return err
		}

		return importWriteError(s.Import(ctx, plan.write))
	})
	if dryRun && errors.Is(err, fs.ErrNotExist) {
		plan, err = planUnstoredImport(in, flag)
	}
	if err != nil {
		return err
	}

	plan.result.DryRun = dryRun
	err = renderImport(o, plan, dryRun || plan.blocked != nil)
	if err != nil || plan.blocked == nil {
		return err
	}

	return plan.blocked
}

// planStoredImport plans in against what s holds, within the timeline flag names when it is set.
// ponytail: the plan reads outside the write transaction, so a writer in between can fail the
// import; plan inside Store.write if concurrent imports matter.
func planStoredImport(ctx context.Context, s *store.Store, in importInput, flag *string) (importPlan, error) {
	if flag != nil {
		guard, err := findTimeline(ctx, s, *flag)
		notFound, ok := errors.AsType[*Error](err)
		if ok && notFound.Kind == KindTimelineNotFound {
			return importPlan{}, noSuchGuard(*flag, notFound.Err)
		}
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

// planUnstoredImport plans in against a database that does not exist yet, which holds no timeline
// for -t to name.
func planUnstoredImport(in importInput, flag *string) (importPlan, error) {
	if flag != nil {
		return importPlan{}, noSuchGuard(*flag, nil)
	}

	return planImport(in)
}

// noSuchGuard returns the timeline_not_found error for -t naming name, wrapping err.
func noSuchGuard(name string, err error) error {
	return &Error{
		Kind: KindTimelineNotFound, Message: fmt.Sprintf("-t %q: no such timeline", name),
		Hint: "Run 'coroner timeline list' to see the timelines.", Err: err,
	}
}

// importWriteError returns id_conflict when another write added a record the import holds, and
// any other error as it is.
func importWriteError(err error) error {
	if !errors.Is(err, store.ErrConflict) {
		return err
	}

	return &Error{
		Kind: KindIDConflict, Message: "the database changed during the import, so nothing was imported",
		Hint: "Another write changed the database; import again.", Err: err,
	}
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
// reads a terminal stdin.
func readImportRecords(
	path string, readFile func(string) ([]byte, error), stdin io.Reader, stdinIsTTY bool,
) ([]export.Record, error) {
	switch {
	case path == "-" && stdinIsTTY:
		return nil, &Error{
			Kind: KindUsage, Message: "- reads the export from stdin, which is a terminal",
			Hint: "Pipe an export in, as in: cat inc-4821.jsonl | coroner import -",
		}
	case path != "-":
		data, err := readFile(path)
		if err != nil {
			return nil, &Error{
				Kind: KindUsage, Message: "cannot read " + path, Err: err,
				Hint: "Pass an export file that exists and you can read, or - to read one from stdin.",
			}
		}
		stdin = bytes.NewReader(data)
	}

	records, err := export.Decode(stdin)
	if invalid, ok := errors.AsType[*export.DecodeError](err); ok {
		return nil, &Error{Kind: KindInvalidInput, Message: invalid.Error(), Hint: invalidImportHint, Err: err}
	}
	if err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}

	return records, nil
}

// readSidecars returns the content of the attachments' files in the attachments/ directory beside
// the export at path, by sha256, which is a file's name up to its first dot. It leaves out files
// that are not there; stdin has no directory beside it.
// ponytail: every sidecar file is read into memory, once per sha256; stream them into the store if
// attachments grow large.
func readSidecars(
	path string, records []export.Record, readDir func(string) ([]fs.DirEntry, error), readFile func(string) ([]byte, error),
) (map[string][]byte, error) {
	sidecars := map[string][]byte{}
	if path == "-" {
		return sidecars, nil
	}
	dir := filepath.Join(filepath.Dir(path), "attachments")
	entries, err := readDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return sidecars, nil
	case err != nil:
		return nil, unreadableSidecar(dir, err)
	}

	files := map[string]string{}
	for _, e := range entries {
		sha256, _, _ := strings.Cut(e.Name(), ".")
		files[sha256] = filepath.Join(dir, e.Name())
	}
	for _, r := range records {
		a, ok := r.(export.Attachment)
		if !ok {
			continue
		}
		file, found := files[a.SHA256]
		if _, read := sidecars[a.SHA256]; !found || read {
			continue
		}
		sidecars[a.SHA256], err = readFile(file)
		if err != nil {
			return nil, unreadableSidecar(file, err)
		}
	}

	return sidecars, nil
}

func unreadableSidecar(path string, err error) error {
	return &Error{Kind: KindUsage, Message: "cannot read " + path, Hint: "Check its permissions, then import again.", Err: err}
}

// localBlobs reports, by sha256, whether each attachment in records has its blob beside the
// database at dbPath.
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

// readStoredRecords returns every stored timeline and source, the stored events, annotations,
// attachments and links whose ids records name, and the stored links joining a link record's
// events with its kind.
// ponytail: one read per id; a batched read if large imports read slowly.
func readStoredRecords(ctx context.Context, s *store.Store, records []export.Record) (storedRecords, error) {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return storedRecords{}, err
	}
	stored := storedRecords{
		timelines: map[string]timeline.Timeline{}, events: map[string]timeline.Event{},
		annotations: map[string]timeline.Annotation{}, attachments: map[string]timeline.Attachment{}, links: map[string]timeline.Link{},
	}
	for _, t := range timelines {
		stored.timelines[t.ID] = t
	}
	stored.sources, err = s.Sources(ctx)
	if err != nil {
		return storedRecords{}, err
	}

	for _, r := range records {
		switch r := r.(type) {
		case export.Event:
			err = addStored(ctx, &r.ID, stored.events, s.Event)
		case export.Annotation:
			err = errors.Join(addStored(ctx, &r.ID, stored.annotations, s.Annotation), addStored(ctx, r.EventID, stored.events, s.Event))
		case export.Attachment:
			err = errors.Join(
				addStored(ctx, &r.ID, stored.attachments, s.Attachment),
				addStored(ctx, r.EventID, stored.events, s.Event), addStored(ctx, r.AnnotationID, stored.annotations, s.Annotation),
			)
		case export.Link:
			err = errors.Join(
				addStored(ctx, &r.ID, stored.links, s.Link), addStoredJoining(ctx, s, r, stored.links),
				addStored(ctx, &r.FromEventID, stored.events, s.Event), addStored(ctx, &r.ToEventID, stored.events, s.Event),
			)
		}
		if err != nil {
			return storedRecords{}, err
		}
	}

	return stored, nil
}

// addStoredJoining adds the stored link of l's kind between l's events to into, under its own id,
// unless none is stored.
func addStoredJoining(ctx context.Context, s *store.Store, l export.Link, into map[string]timeline.Link) error {
	stored, err := s.LinkBetween(ctx, l.FromEventID, l.ToEventID, l.Kind)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	into[stored.ID] = stored

	return nil
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

// renderImport says on stderr what plan did, or would do when unwritten, and prints its result
// object under --json.
func renderImport(o output, plan importPlan, unwritten bool) error {
	verb := "Imported"
	if unwritten {
		verb = "Would import"
	}
	r := plan.result
	lines := make([]string, 0, 8+len(r.Conflicts))
	if n := len(plan.timelineNames); n > 0 {
		noun := "timelines"
		if n == 1 {
			noun = "timeline"
		}
		lines = append(lines, fmt.Sprintf("%s into %s `%s`", verb, noun, strings.Join(plan.timelineNames, "`, `")))
	}
	for _, c := range []struct {
		name   string
		counts importCounts
	}{
		{"timelines", r.Timelines},
		{"sources", r.Sources},
		{"events", r.Events},
		{"annotations", r.Annotations},
		{"attachments", r.Attachments},
		{"links", r.Links},
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
			"Left out %d %s whose files are neither beside the export nor in the database; import an export written with -o <directory>/ to bring them",
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
