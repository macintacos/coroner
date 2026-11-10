package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
)

func newInternalLogCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Print the diagnostic log as text",
		Long: "Print the diagnostic log beside the database as text, oldest line first: <log>.1, then\n" +
			"<log>. Each JSON line prints as one key=value line, as --debug prints it on stderr, with\n" +
			"its time in --tz, then the config file's default_timezone, then UTC. A line that is not a\n" +
			"JSON log line prints as it is, whatever the filters. --since is inclusive and --until\n" +
			"exclusive. Scripts read the JSON file itself; this text may change in any release.",
		Example: "  $ coroner __internal log\n" +
			"  $ coroner __internal log --level warn\n" +
			"  $ coroner __internal log --command \"event add\" --since \"2026-09-11T03:00Z\" --until \"2026-09-11T04:00Z\"",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			inv, err := readInvocation(cmd, app)
			if err != nil {
				return err
			}
			z, err := inv.zone()
			if err != nil {
				return err
			}
			keep, err := readLogFilter(cmd, inv.now(), z)
			if err != nil {
				return err
			}

			return printLog(ctx, app, inv.out, config.DiagnosticLogPath(inv.cfg.DBPath), newLogPrinter(app.Out, keep, z.loc))
		},
	}

	flags := cmd.Flags()
	flags.String("level", "", "Keep lines at this `level` and above: {debug | info | warn | error}")
	flags.String("command", "", "Keep lines from this command `path` and the commands under it, as \"event add\"")
	flags.String("since", "", "Keep lines at or after this `time`")
	flags.String("until", "", "Keep lines before this `time`")

	return cmd
}

// printLog prints the log at path, its rotated <path>.1 first, or says on o's stderr that
// neither exists.
func printLog(ctx context.Context, app App, o output, path string, p logPrinter) error {
	printed := false
	for _, name := range []string{path + ".1", path} {
		found, err := printLogFile(ctx, app, name, p)
		if err != nil {
			return err
		}
		printed = printed || found
	}
	if printed {
		return nil
	}

	return o.notify("No diagnostic log at %s yet; coroner writes it once a command opens that database.", path)
}

// printLogFile prints the file at name, reporting false when it does not exist.
func printLogFile(ctx context.Context, app App, name string, p logPrinter) (bool, error) {
	file, err := app.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("opening %s: %w", name, err)
	}
	err = errors.Join(p.write(ctx, file), file.Close())
	if err != nil {
		return true, fmt.Errorf("reading %s: %w", name, err)
	}

	return true, nil
}

// logFilter keeps the log lines its set fields allow.
type logFilter struct {
	level        *slog.Level
	command      string
	since, until *time.Time
}

func (f *logFilter) keeps(line logLine) bool {
	return (f.level == nil || line.level >= *f.level) &&
		(f.command == "" || line.command == f.command || strings.HasPrefix(line.command, f.command+" ")) &&
		(f.since == nil || !line.time.Before(*f.since)) &&
		(f.until == nil || line.time.Before(*f.until))
}

// readLogFilter returns the filter the flags ask for, rejecting it before any file opens.
func readLogFilter(cmd *cobra.Command, now time.Time, z zone) (logFilter, error) {
	flags := cmd.Flags()
	var f logFilter
	err := f.readLevel(changedString(flags, "level"))
	if err != nil {
		return f, err
	}
	f.command, err = readCommandPath(cmd.Root(), changedString(flags, "command"))
	if err != nil {
		return f, err
	}
	f.since, _, err = readBound(flags, "since", now, z)
	if err != nil {
		return f, err
	}
	f.until, _, err = readBound(flags, "until", now, z)

	return f, err
}

// readLevel sets f's level to the one value names, leaving it nil when value is nil.
func (f *logFilter) readLevel(value *string) error {
	if value == nil {
		return nil
	}

	var level slog.Level
	err := level.UnmarshalText([]byte(*value))
	if err != nil {
		return &Error{
			Kind: KindUsage, Message: fmt.Sprintf("--level %q is not debug, info, warn or error", *value), Hint: "Pass --level debug, info, warn or error.",
		}
	}
	f.level = &level

	return nil
}

// readCommandPath returns the path of the command value names below root, "" when value is nil.
func readCommandPath(root *cobra.Command, value *string) (string, error) {
	if value == nil {
		return "", nil
	}

	found, rest, err := root.Find(strings.Fields(*value))
	if err != nil || len(rest) > 0 || found == root {
		return "", &Error{
			Kind: KindUsage, Message: fmt.Sprintf("--command %q names no coroner command", *value), Hint: "Pass a command path below coroner, as --command \"event add\".",
		}
	}

	return found.CommandPath(), nil
}

// logLine is one diagnostic log line, its attributes in the order the line holds them.
type logLine struct {
	time    time.Time
	level   slog.Level
	msg     string
	command string
	attrs   []slog.Attr
}

// parseLogLine reads raw as a JSON log line, reporting false when it is not one.
func parseLogLine(raw []byte) (logLine, bool) {
	var line logLine
	if !json.Valid(raw) {
		return line, false
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	start, _ := decoder.Token()
	if start != json.Delim('{') {
		return line, false
	}

	seen := map[string]bool{}
	for decoder.More() {
		key, _ := decoder.Token()
		var value any
		_ = decoder.Decode(&value)
		name, _ := key.(string)
		seen[name] = line.add(name, value)
	}

	return line, seen["time"] && seen["level"] && seen["msg"]
}

// add sets the field or attribute key names, reporting whether a time, level or msg parsed.
func (l *logLine) add(key string, value any) bool {
	text, isText := value.(string)
	switch key {
	case "time":
		t, err := time.Parse(time.RFC3339Nano, text)
		l.time = t
		return isText && err == nil
	case "level":
		return isText && l.level.UnmarshalText([]byte(text)) == nil
	case "msg":
		l.msg = text
		return isText
	case "command":
		l.command = text
	case "duration":
		n, isNumber := value.(json.Number)
		nanos, err := n.Int64()
		if isNumber && err == nil {
			l.attrs = append(l.attrs, slog.Duration(key, time.Duration(nanos)))
			return false
		}
	}
	l.attrs = append(l.attrs, slog.Any(key, value))

	return false
}

// logPrinter prints the diagnostic log lines keep keeps to out as text, times in loc.
type logPrinter struct {
	out  io.Writer
	text slog.Handler
	keep logFilter
	loc  *time.Location
}

func newLogPrinter(out io.Writer, keep logFilter, loc *time.Location) logPrinter {
	return logPrinter{out: out, text: slog.NewTextHandler(out, nil), keep: keep, loc: loc}
}

// write reads r line by line, writing each kept line before reading the next.
func (p logPrinter) write(ctx context.Context, r io.Reader) error {
	reader := bufio.NewReader(r)
	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			writeErr := p.writeLine(ctx, bytes.TrimSuffix(raw, []byte("\n")))
			if writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p logPrinter) writeLine(ctx context.Context, raw []byte) error {
	line, ok := parseLogLine(raw)
	if !ok {
		var b strings.Builder
		writeEscaped(&b, string(raw))
		b.WriteByte('\n')
		_, err := io.WriteString(p.out, b.String())
		return err
	}
	if !p.keep.keeps(line) {
		return nil
	}

	record := slog.NewRecord(line.time.In(p.loc), line.level, line.msg, 0)
	record.AddAttrs(line.attrs...)

	return p.text.Handle(ctx, record)
}
