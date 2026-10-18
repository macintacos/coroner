package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type format string

const (
	formatText  format = "text"
	formatJSON  format = "json"
	formatJSONL format = "jsonl"
)

// formatsAnnotation is the cmd.Annotations key listing the formats a command prints,
// comma-separated, as in "text,json". A command without it prints text only.
const formatsAnnotation = "formats"

func (f format) machine() bool {
	return f == formatJSON || f == formatJSONL
}

// outputFormat returns the format --format and --json select, rejecting one cmd does not
// declare in its formatsAnnotation.
func outputFormat(cmd *cobra.Command) (format, error) {
	flags := cmd.Flags()
	chosen, _ := flags.GetString("format")

	if asJSON, _ := flags.GetBool("json"); asJSON {
		if flags.Changed("format") && format(chosen) != formatJSON {
			return "", &Error{
				Kind:    KindUsage,
				Message: "--json conflicts with --format " + chosen,
				Hint:    "Pass --json or --format, not both.",
			}
		}

		chosen = string(formatJSON)
	}

	supported := strings.Split(cmp.Or(cmd.Annotations[formatsAnnotation], string(formatText)), ",")
	if !slices.Contains(supported, chosen) {
		return "", &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("%s cannot print --format %s", cmd.CommandPath(), chosen),
			Hint:    "Use --format " + strings.Join(supported, " or --format ") + ".",
		}
	}

	return format(chosen), nil
}

// writeJSON writes v as one JSON document, indented when pretty.
func writeJSON(w io.Writer, pretty bool, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}

	return encoder.Encode(v)
}
