// Command release does the deterministic steps of a coroner release for the /release-coroner
// skill: each subcommand prints exactly one JSON object, {"ok":true,...} on success or
// {"ok":false,"error":{"code","message"}} on failure, and exits 0, 1 on a failure, or 2 on a
// usage error. Under go run every non-zero exit becomes 1, so callers branch on ok and
// error.code. Run it from the module root.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	repo      = "macintacos/coroner"
	usageLine = "usage: go run ./internal/tools/release gates | compute <patch|minor|major> [--rc] --sha <rev> | tag --tag <tag> --sha <rev> --notes-file <path> [--dry-run] | tag-notes --tag <tag> --out <path> | watch --tag <tag> --sha <rev> | verify --tag <tag>"
)

func main() {
	os.Exit(runCommand(context.Background(), realSystem(os.Stderr), os.Args[1:], os.Stdout))
}

// toolError is a failure the tool reports in its JSON envelope.
type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *toolError) Error() string { return e.Message }

func usageError(format string, args ...any) error {
	return &toolError{Code: "usage", Message: fmt.Sprintf(format, args...)}
}

// runCommand runs the subcommand args names, prints its one JSON object to stdout, and returns
// the exit code.
func runCommand(ctx context.Context, sys system, args []string, stdout io.Writer) int {
	result, err := dispatch(ctx, sys, args)
	if err == nil {
		writeJSON(stdout, result)
		return 0
	}

	failure, ok := errors.AsType[*toolError](err)
	if !ok {
		failure = &toolError{Code: "internal", Message: err.Error()}
	}
	writeJSON(stdout, map[string]any{"ok": false, "error": failure})
	if failure.Code == "usage" {
		return 2
	}

	return 1
}

func dispatch(ctx context.Context, sys system, args []string) (any, error) {
	if len(args) == 0 {
		return nil, usageError("%s", usageLine)
	}

	switch args[0] {
	case "compute":
		return compute(ctx, sys, args[1:])
	case "gates":
		return gates(ctx, sys, args[1:])
	case "tag":
		return tag(ctx, sys, args[1:])
	case "tag-notes":
		return tagNotes(ctx, sys, args[1:])
	case "verify":
		return verify(ctx, sys, args[1:])
	case "watch":
		return watch(ctx, sys, args[1:])
	}

	return nil, usageError("unknown subcommand %q; %s", args[0], usageLine)
}

func writeJSON(w io.Writer, v any) {
	_ = json.MarshalWrite(w, v)
	_, _ = io.WriteString(w, "\n")
}
