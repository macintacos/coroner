package cli

import (
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

// outputFormat returns the format --format and --json select, rejecting one cmd cannot
// produce.
func outputFormat(cmd *cobra.Command, supported ...format) (format, error) {
	flags := cmd.Flags()
	chosen := format(flags.Lookup("format").Value.String())

	if asJSON, _ := flags.GetBool("json"); asJSON {
		if flags.Changed("format") && chosen != formatJSON {
			return "", &Error{
				Kind:    KindUsage,
				Message: "--json conflicts with --format " + string(chosen),
				Hint:    "Pass --json or --format, not both.",
			}
		}

		chosen = formatJSON
	}

	if !slices.Contains(supported, chosen) {
		options := make([]string, len(supported))
		for i, f := range supported {
			options[i] = "--format " + string(f)
		}

		return "", &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("%s cannot print --format %s", cmd.CommandPath(), chosen),
			Hint:    "Use " + strings.Join(options, " or ") + ".",
		}
	}

	return chosen, nil
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
