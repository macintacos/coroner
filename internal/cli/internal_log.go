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
			filter, err := readLogFilter(cmd, inv.now(), z)
			if err != nil {
				return err
			}

			path := config.DiagnosticLogPath(inv.cfg.DBPath)
			p := logPrinter{open: app.Open, out: inv.out.out, keep: filter, loc: z.loc}
			printed, err := p.printFiles(ctx, path+".1", path)
			if err != nil || printed {
				return err
			}

			return inv.out.notify("No diagnostic log at %s yet; coroner writes it once a command opens that database.", path)
		},
	}

	flags := cmd.Flags()
	flags.String("level", "", "Keep lines at this `level` and above: {debug | info | warn | error}")
	flags.String("command", "", "Keep lines from this command `path` and the commands under it, as \"event add\"")
	flags.String("since", "", "Keep lines at or after this `time`")
	flags.String("until", "", "Keep lines before this `time`")

	return cmd
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

// readLogFilter returns the filter cmd's flags ask for.
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return line, false
	}

	var has struct{ time, level, msg bool }
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return line, false
		}
		var value any
		err = decoder.Decode(&value)
		if err != nil {
			return line, false
		}
		name, ok := key.(string)
		if !ok {
			return line, false
		}
		switch name {
		case "time":
			line.time, has.time = parseLogTime(value)
		case "level":
			line.level, has.level = parseLogLevel(value)
		case "msg":
			line.msg, has.msg = value.(string)
		default:
			line.addAttr(name, value)
		}
	}

	return line, has.time && has.level && has.msg && endsAfterObject(decoder)
}

// endsAfterObject reports whether decoder holds nothing past the object's closing brace.
func endsAfterObject(decoder *json.Decoder) bool {
	_, err := decoder.Token()
	if err != nil {
		return false
	}
	_, err = decoder.Token()

	return errors.Is(err, io.EOF)
}

func parseLogTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	t, err := time.Parse(time.RFC3339Nano, text)

	return t, ok && err == nil
}

func parseLogLevel(value any) (slog.Level, bool) {
	var level slog.Level
	text, ok := value.(string)

	return level, ok && level.UnmarshalText([]byte(text)) == nil
}

// addAttr appends the attribute key names, noting the command for the filter.
func (l *logLine) addAttr(key string, value any) {
	if key == "command" {
		l.command, _ = value.(string)
	}
	// slog's JSON handler writes a time.Duration as integer nanoseconds.
	if n, ok := value.(json.Number); ok && key == "duration" {
		nanos, err := n.Int64()
		if err == nil {
			l.attrs = append(l.attrs, slog.Duration(key, time.Duration(nanos)))
			return
		}
	}
	l.attrs = append(l.attrs, slog.Any(key, value))
}

// logPrinter prints diagnostic log files as text: the lines keep keeps, times in loc.
type logPrinter struct {
	open func(name string) (io.ReadCloser, error)
	out  io.Writer
	keep logFilter
	loc  *time.Location
	// textHandler is set by printFiles.
	textHandler slog.Handler
}

// printFiles prints each of names that exists, in order, reporting whether any did.
func (p logPrinter) printFiles(ctx context.Context, names ...string) (bool, error) {
	p.textHandler = slog.NewTextHandler(p.out, nil)
	printed := false
	for _, name := range names {
		found, err := p.printFile(ctx, name)
		if err != nil {
			return printed, err
		}
		printed = printed || found
	}

	return printed, nil
}

// printFile prints the file at name, reporting false when it does not exist.
func (p logPrinter) printFile(ctx context.Context, name string) (bool, error) {
	file, err := p.open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, unreadableLog(name, err)
	}

	return true, errors.Join(p.printLines(ctx, file, name), file.Close())
}

func unreadableLog(name string, err error) *Error {
	return &Error{Kind: KindUsage, Message: "cannot read " + name, Hint: "Check that the path is a file you can read.", Err: err}
}

// printLines reads the file name from r line by line, writing each kept line before reading the next.
func (p logPrinter) printLines(ctx context.Context, r io.Reader, name string) error {
	reader := bufio.NewReader(r)
	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			writeErr := p.printLine(ctx, bytes.TrimSuffix(raw, []byte("\n")))
			if writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return unreadableLog(name, err)
		}
	}
}

func (p logPrinter) printLine(ctx context.Context, raw []byte) error {
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

	// Handle, unlike a Logger, never checks the handler's level, so debug lines print.
	return p.textHandler.Handle(ctx, record)
}
