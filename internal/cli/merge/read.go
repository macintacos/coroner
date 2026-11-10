package merge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
)

// InvalidImportError returns problems as one invalid_input error: how many records they name,
// each problem's text before its first ": ", then the first few problems and
// how many more there are, with hint.
func InvalidImportError(problems []string, hint string) *clikit.Error {
	records := map[string]bool{}
	for _, p := range problems {
		record, _, _ := strings.Cut(p, ": ")
		records[record] = true
	}
	header := fmt.Sprintf("%d records cannot be imported:", len(records))
	if len(records) == 1 {
		header = "1 record cannot be imported:"
	}
	lines := append([]string{header}, clikit.FirstFew(problems, ShownImportProblems)...)

	return &clikit.Error{Kind: clikit.KindInvalidInput, Message: strings.Join(lines, "\n"), Hint: hint}
}

// InvalidImportHint is the hint of an import that invalid records stopped.
const InvalidImportHint = "Nothing was imported. Fix those records, or export the file again."

// LogImportPlan logs, for each record type and action, the count at info and each record's id
// at debug.
func LogImportPlan(ctx context.Context, logger *slog.Logger, decisions []ImportDecision) {
	for _, typ := range []string{"timeline", "source", "event", "annotation", "attachment", "link"} {
		for _, action := range []importAction{importInserted, importSkipped, importReplaced} {
			clikit.LogCounted(ctx, logger, typ+"s "+string(action), clikit.IDKey(typ), decidedIDs(decisions, typ, action))
		}
	}
}

// decidedIDs returns the ids of the records of type typ the plan decided action for.
func decidedIDs(decisions []ImportDecision, typ string, action importAction) []string {
	var ids []string
	for _, d := range decisions {
		if d.typ == typ && d.action == action {
			ids = append(ids, d.id)
		}
	}
	return ids
}

// JSONLSource is where JSONL records come from: the file at path, or stdin when path is -.
type JSONLSource struct {
	Path       string
	ReadFile   func(string) ([]byte, error)
	Stdin      io.Reader
	StdinIsTTY bool
}

// ReadImportRecords reads and decodes the export src names, putting hint on a decode error. It
// never reads a terminal stdin.
func ReadImportRecords(logger *slog.Logger, src JSONLSource, hint string) ([]export.Record, error) {
	path, stdin := src.Path, src.Stdin
	switch {
	case path == "-" && src.StdinIsTTY:
		return nil, &clikit.Error{
			Kind: clikit.KindUsage, Message: "- reads the export from stdin, which is a terminal",
			Hint: "Pipe an export in, as in: cat inc-4821.jsonl | coroner import -",
		}
	case path != "-":
		data, err := src.ReadFile(path)
		if err != nil {
			return nil, &clikit.Error{
				Kind: clikit.KindUsage, Message: "cannot read " + path, Err: err,
				Hint: "Pass an export file that exists and you can read, or - to read one from stdin.",
			}
		}
		stdin = bytes.NewReader(data)
	}

	records, stats, err := export.Decode(stdin)
	if invalid, ok := errors.AsType[*export.DecodeError](err); ok {
		return nil, &clikit.Error{Kind: clikit.KindInvalidInput, Message: invalid.Error(), Hint: hint, Err: err}
	}
	if err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}
	logger.Debug("export decoded", "path", src.Path, "lines", stats.Lines, "format_versions", stats.FormatVersions)

	return records, nil
}
