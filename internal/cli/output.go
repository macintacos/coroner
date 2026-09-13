package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type format string

const (
	formatText  format = "text"
	formatJSON  format = "json"
	formatJSONL format = "jsonl"
	// formatMarkdown and formatMermaid are not machine(), so errors under them print as text.
	formatMarkdown format = "md"
	formatMermaid  format = "mermaid"
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

// output is how and where a command prints, read once from its flags and App.
type output struct {
	format        format
	quiet, outTTY bool
	out, errOut   io.Writer
}

func newOutput(cmd *cobra.Command, app App) (output, error) {
	chosen, err := outputFormat(cmd)
	if err != nil {
		return output{}, err
	}
	quiet, _ := cmd.Flags().GetBool("quiet")

	return output{format: chosen, quiet: quiet, outTTY: app.OutIsTTY, out: app.Out, errOut: app.Err}, nil
}

// notify writes a message meant for people, and a newline, to o.errOut unless --quiet was given.
func (o output) notify(message string, args ...any) error {
	if o.quiet {
		return nil
	}

	_, err := fmt.Fprintf(o.errOut, message+"\n", args...)
	return err
}

func (o output) json(v any) error {
	return writeJSON(o.out, o.outTTY, v)
}

// rows writes one line per row, aligned on a terminal and tab-separated otherwise. Tabs and
// line breaks inside a field print escaped, so no field splits a row.
func (o output) rows(rows [][]string) error {
	escape := strings.NewReplacer("\t", `\t`, "\n", `\n`, "\r", `\r`)
	var b strings.Builder
	for _, row := range rows {
		for i, field := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			_, _ = escape.WriteString(&b, field)
		}
		b.WriteByte('\n')
	}

	if !o.outTTY {
		_, err := io.WriteString(o.out, b.String())
		return err
	}

	table := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	_, err := io.WriteString(table, b.String())
	if err != nil {
		return err
	}

	return table.Flush()
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
