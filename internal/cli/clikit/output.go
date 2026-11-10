package clikit

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Format is an output format --format names.
type Format string

// The output formats. Markdown, Mermaid, HTML and SVG are not Machine, so errors under them
// print as text.
const (
	FormatText     Format = "text"
	FormatJSON     Format = "json"
	FormatJSONL    Format = "jsonl"
	FormatMarkdown Format = "md"
	FormatMermaid  Format = "mermaid"
	FormatHTML     Format = "html"
	FormatSVG      Format = "svg"
)

// FormatsAnnotation is the cmd.Annotations key listing the formats a command prints,
// comma-separated, as in "text,json". A command without it prints text only.
const FormatsAnnotation = "formats"

// Machine reports whether scripts parse the format, so errors under it print as JSON too.
func (f Format) Machine() bool {
	return f == FormatJSON || f == FormatJSONL
}

// OutputFormat returns the format --format and --json select, rejecting one cmd does not
// declare in its FormatsAnnotation.
func OutputFormat(cmd *cobra.Command) (Format, error) {
	flags := cmd.Flags()
	chosen, _ := flags.GetString("format")

	if asJSON, _ := flags.GetBool("json"); asJSON {
		if flags.Changed("format") && Format(chosen) != FormatJSON {
			return "", &Error{
				Kind:    KindUsage,
				Message: "--json conflicts with --format " + chosen,
				Hint:    "Pass --json or --format, not both.",
			}
		}

		chosen = string(FormatJSON)
	}

	supported := strings.Split(cmp.Or(cmd.Annotations[FormatsAnnotation], string(FormatText)), ",")
	if !slices.Contains(supported, chosen) {
		return "", &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("%s cannot print --format %s", cmd.CommandPath(), chosen),
			Hint:    "Use --format " + strings.Join(supported, " or --format ") + ".",
		}
	}

	return Format(chosen), nil
}

// Output is how and where a command prints, read once from its flags and App.
type Output struct {
	Format        Format
	quiet, OutTTY bool
	Out, errOut   io.Writer
}

func newOutput(cmd *cobra.Command, app App) (Output, error) {
	chosen, err := OutputFormat(cmd)
	if err != nil {
		return Output{}, err
	}
	quiet, _ := cmd.Flags().GetBool("quiet")

	return Output{Format: chosen, quiet: quiet, OutTTY: app.OutIsTTY, Out: app.Out, errOut: app.Err}, nil
}

// Notify writes a message meant for people, and a newline, to o.errOut unless --quiet was given.
func (o Output) Notify(message string, args ...any) error {
	if o.quiet {
		return nil
	}

	_, err := fmt.Fprintf(o.errOut, message+"\n", args...)
	return err
}

// PrintID writes the id of the record a create made or found on its own line to o.out under
// --quiet with text output; under --json the object carries it instead.
func (o Output) PrintID(id string) error {
	if !o.quiet || o.Format != FormatText {
		return nil
	}

	_, err := fmt.Fprintln(o.Out, id)
	return err
}

// WriteTo runs write against o when path is -. Otherwise it runs write against o as piped
// output into a buffer, writes the buffer to path with writeFile, and says so. It returns the
// size in bytes of the file it wrote, or 0 when path is -.
func (o Output) WriteTo(path string, writeFile func(name string, data []byte, perm fs.FileMode) error, write func(Output) error) (int, error) {
	if path == "-" {
		return 0, write(o)
	}

	var buffer bytes.Buffer
	piped := o
	piped.Out, piped.OutTTY = &buffer, false
	err := write(piped)
	if err != nil {
		return 0, err
	}
	err = writeFile(path, buffer.Bytes(), 0o644)
	if err != nil {
		return 0, &Error{
			Kind: KindUsage, Message: "cannot write " + path, Err: err,
			Hint: "Pass -o a file in a directory that exists and you can write to, or -o - for stdout.",
		}
	}

	return buffer.Len(), o.Notify("Wrote %s", path)
}

// JSON writes v as one JSON document on Out.
func (o Output) JSON(v any) error {
	return WriteJSON(o.Out, o.OutTTY, v)
}

// Rows writes one line per row, aligned on a terminal and tab-separated otherwise. Tabs and
// line breaks inside a field print escaped, so no field splits a row, and so does every other
// control character, so stored text cannot drive the terminal.
func (o Output) Rows(rows [][]string) error {
	var b strings.Builder
	for _, row := range rows {
		for i, field := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			WriteEscaped(&b, field)
		}
		b.WriteByte('\n')
	}

	if !o.OutTTY {
		_, err := io.WriteString(o.Out, b.String())
		return err
	}

	table := tabwriter.NewWriter(o.Out, 0, 0, 2, ' ', 0)
	_, err := io.WriteString(table, b.String())
	if err != nil {
		return err
	}

	return table.Flush()
}

// WriteEscaped writes field to b with tab, LF and CR as \t, \n and \r, and every other C0
// control, DEL and C1 control as a Go escape.
func WriteEscaped(b *strings.Builder, field string) {
	for _, r := range field {
		switch {
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(b, `\x%02x`, r)
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
}

// WriteJSON writes v as one JSON document, indented when pretty.
func WriteJSON(w io.Writer, pretty bool, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}

	return encoder.Encode(v)
}

// FirstFew returns the first shown items, followed by "and N more" when it leaves some out.
func FirstFew(items []string, shown int) []string {
	few := slices.Clone(items[:min(len(items), shown)])
	if more := len(items) - shown; more > 0 {
		few = append(few, fmt.Sprintf("and %d more", more))
	}

	return few
}

// CodeList returns names as code spans, joined by commas.
func CodeList(names []string) string {
	return "`" + strings.Join(names, "`, `") + "`"
}

// PluralNoun returns noun, with an s unless n is 1.
func PluralNoun(n int, noun string) string {
	if n == 1 {
		return noun
	}

	return noun + "s"
}

// OutputPath returns the file -o names, - meaning stdout. --format html requires -o: writing
// the page is explicit (OUT-8).
func OutputPath(flags *pflag.FlagSet, chosen Format) (string, error) {
	path, _ := flags.GetString("output")
	switch {
	case chosen == FormatHTML && !flags.Changed("output"):
		return "", &Error{Kind: KindUsage, Message: "--format html needs -o", Hint: "Pass -o inc-4821.html, or -o - to write the page to stdout."}
	case path == "":
		return "", &Error{Kind: KindUsage, Message: "-o must not be empty", Hint: "Pass -o <file>, or -o - for stdout."}
	}

	return path, nil
}

// WriteField writes a label and its value as one aligned line of a record's detail view.
func WriteField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%-13s%s\n", label, value)
}
