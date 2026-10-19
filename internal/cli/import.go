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
)

func newImportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "import {<file> | -}",
		Short:   "Import records from a JSONL export",
		GroupID: coreCommands,
		Long: "Import the records of a coroner JSONL export, as 'coroner timeline export --format jsonl'\n" +
			"writes it, into the database: its timelines, sources, events, annotations, attachments and\n" +
			"links. Pass - to read the export from stdin, so concatenated exports import together. Every\n" +
			"record is checked before anything is written, and the import is written in one transaction.\n\n" +
			"Records match by id, and sources by name. A record stored unchanged is skipped, so importing a\n" +
			"file twice changes nothing. A record stored with different content is a conflict: by default\n" +
			"the import writes nothing and exits 4; --on-conflict skip keeps the stored record, and\n" +
			"--on-conflict replace takes the import's. Replace never changes an attachment or link, or moves\n" +
			"an event or annotation, so such a conflict exits 4 under replace too. Authors and created and\n" +
			"updated times come from the export. An imported event keeps its seq unless its timeline has\n" +
			"already reached that seq, when it gets the next one.\n\n" +
			"An attachment is imported with its file from the attachments/ directory beside the export,\n" +
			"which 'coroner timeline export -o <directory>/' writes, or when the database already has that\n" +
			"file. Otherwise it is left out and listed.\n\n" +
			"-t names a stored timeline that every record must belong to. Without -t, the export decides\n" +
			"which timelines it imports into.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json", timelinesAnnotation: "guard"},
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
	policy, err := readConflictPolicy(cmd.Flags())
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	inv, err := readInvocation(cmd, app)
	if err != nil {
		return err
	}
	records, err := readImportRecords(jsonlSource{path: path, readFile: app.ReadFile, stdin: app.In, stdinIsTTY: app.InIsTTY}, invalidImportHint)
	if err != nil {
		return err
	}
	// Read before the store opens: withStoreOpenedBy would report a permission error on either as
	// the database's.
	sidecars, err := readSidecars(path, records, app.ReadDir, app.ReadFile)
	if err != nil {
		return err
	}

	in := mergeInput{
		records: records, sidecars: sidecars, localBlobs: localBlobs(inv.cfg.DBPath, records, app.Stat),
		onConflict: policy, newID: app.NewID, now: app.Now(),
		timelines: inv.timelines, lookupEnv: app.LookupEnv,
	}
	plan, err := mergeImport(ctx, app, inv.cfg.DBPath, in, dryRun)
	if err != nil {
		return err
	}
	if len(plan.problems) > 0 {
		return invalidImportError(plan.problems, invalidImportHint)
	}
	if plan.takenName != "" {
		return timelineNameTakenError(plan.takenName)
	}

	plan.result.DryRun = dryRun
	err = renderImport(inv.out, plan, dryRun || !plan.canWrite())
	if err != nil {
		return err
	}

	return importBlockedError(plan)
}

// invalidImportError returns problems as one invalid_input error: a count, the first few, and
// how many more there are, with hint.
func invalidImportError(problems []string, hint string) *Error {
	header := fmt.Sprintf("%d records cannot be imported:", len(problems))
	if len(problems) == 1 {
		header = "1 record cannot be imported:"
	}
	lines := append([]string{header}, problems[:min(len(problems), shownImportProblems)]...)
	if more := len(problems) - shownImportProblems; more > 0 {
		lines = append(lines, fmt.Sprintf("and %d more", more))
	}

	return &Error{Kind: KindInvalidInput, Message: strings.Join(lines, "\n"), Hint: hint}
}

const invalidImportHint = "Nothing was imported. Fix those records, or export the file again."

// timelineNameTakenError returns timeline_exists for an incoming timeline named name, which a
// stored timeline with another id holds.
func timelineNameTakenError(name string) *Error {
	return &Error{
		Kind:    KindTimelineExists,
		Message: fmt.Sprintf("timeline `%s` already exists here as another timeline, so nothing was imported", name),
		Hint:    fmt.Sprintf("Rename it with 'coroner timeline edit %s --name <new-name>', then import again.", name),
	}
}

// importBlockedError returns the id_conflict that stops plan from writing, or nil when nothing
// blocks it.
func importBlockedError(plan importPlan) error {
	switch plan.blocked {
	case blockConflicts:
		return &Error{
			Kind:    KindIDConflict,
			Message: recordCount(len(plan.result.Conflicts)) + " conflict with what is stored, so nothing was imported",
			Hint:    "Pass --on-conflict skip to keep what is stored, or --on-conflict replace to take the import's.",
		}
	case blockUnreplaceable:
		return &Error{
			Kind: KindIDConflict,
			Message: recordCount(len(plan.unreplaceable)) +
				" cannot be replaced, so nothing was imported: an attachment or link never changes, and an event or annotation never moves",
			Hint: "Pass --on-conflict skip to keep what is stored.",
		}
	case blockNone:
	}

	return nil
}

// mergeImport merges in into the database at dbPath, or plans it only under dryRun, which opens
// the database read-only and plans against an empty one when it does not exist yet.
func mergeImport(ctx context.Context, app App, dbPath string, in mergeInput, dryRun bool) (importPlan, error) {
	open, mode := app.OpenStore, mergeWrite
	if dryRun {
		open, mode = app.OpenStoreReadOnly, mergePlanOnly
	}
	var out mergeOutcome
	err := withStoreOpenedBy(ctx, open, dbPath, func(s *store.Store) error {
		var err error
		out, err = merge(ctx, s, in, mode)
		return err
	})
	if !dryRun || !errors.Is(err, fs.ErrNotExist) {
		return out.plan, err
	}
	err = in.timelines.withoutStore()
	if err != nil {
		return importPlan{}, err
	}

	return planImport(ctx, in.importInput, storedRecords{})
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

// jsonlSource is where JSONL records come from: the file at path, or stdin when path is -.
type jsonlSource struct {
	path       string
	readFile   func(string) ([]byte, error)
	stdin      io.Reader
	stdinIsTTY bool
}

// readImportRecords reads and decodes the export src names, putting hint on a decode error. It
// never reads a terminal stdin.
func readImportRecords(src jsonlSource, hint string) ([]export.Record, error) {
	path, stdin := src.path, src.stdin
	switch {
	case path == "-" && src.stdinIsTTY:
		return nil, &Error{
			Kind: KindUsage, Message: "- reads the export from stdin, which is a terminal",
			Hint: "Pipe an export in, as in: cat inc-4821.jsonl | coroner import -",
		}
	case path != "-":
		data, err := src.readFile(path)
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
		return nil, &Error{Kind: KindInvalidInput, Message: invalid.Error(), Hint: hint, Err: err}
	}
	if err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}

	return records, nil
}

// sidecarBlobs returns the contents of the sidecars whose blob local does not hold.
func sidecarBlobs(sidecars map[string][]byte, local map[string]bool) []io.Reader {
	var blobs []io.Reader
	for sha, content := range sidecars {
		if !local[sha] {
			blobs = append(blobs, bytes.NewReader(content))
		}
	}

	return blobs
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
// ponytail: read before merge's Change, so a garbage collection in between can delete a blob an
// attachment relies on, which Tx.Import reports as not found; stat inside the Change if that race
// matters.
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
