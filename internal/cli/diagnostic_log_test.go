package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// logLines returns the diagnostic log at path, one decoded JSON object per line.
func logLines(path string) []map[string]any {
	GinkgoHelper()
	return decodeLines(readFile(path))
}

// decodeLines decodes written, one JSON object per line.
func decodeLines(written string) []map[string]any {
	GinkgoHelper()
	var lines []map[string]any
	for line := range strings.Lines(written) {
		var decoded map[string]any
		Expect(json.Unmarshal([]byte(line), &decoded)).To(Succeed(), line)
		lines = append(lines, decoded)
	}
	return lines
}

// lastLogLine returns the last line of the diagnostic log at path.
func lastLogLine(path string) map[string]any {
	GinkgoHelper()
	lines := logLines(path)
	Expect(lines).NotTo(BeEmpty())
	return lines[len(lines)-1]
}

type panickingWriter struct{}

func (panickingWriter) Write([]byte) (int, error) { panic("stdout went away") }

// readIfExists returns the file at path, or "" when there is none.
func readIfExists(path string) string {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return string(data)
}

var _ = Describe("diagnostic log", func() {
	var (
		h           harness
		logPath     string
		out, errOut bytes.Buffer
		openedTheDB bool
	)

	BeforeEach(func() {
		out.Reset()
		errOut.Reset()
		dir := GinkgoT().TempDir()
		ids := 0
		h = harness{home: dir, db: filepath.Join(dir, "coroner.db"), out: &out, errOut: &errOut, newID: countedIDs(&ids)}
		logPath = config.DiagnosticLogPath(h.db)
	})

	// run runs args against h, recording whether the command opened the database.
	run := func(ctx context.Context, app cli.App) int {
		openedTheDB = false
		openStore, openReadOnly := app.OpenStore, app.OpenStoreReadOnly
		app.OpenStore = func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error) {
			openedTheDB = true
			return openStore(ctx, path, opts...)
		}
		app.OpenStoreReadOnly = func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error) {
			openedTheDB = true
			return openReadOnly(ctx, path, opts...)
		}
		return cli.Run(ctx, app)
	}

	It("ends every command that opens a database with its finish line, and keeps opted-out commands out", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		Expect(run(ctx, h.app("timeline", "use", "t"))).To(Equal(0))

		optedOut := []string{"coroner version", "coroner agent skill", "coroner completion", "coroner __internal"}
		isOptedOut := func(path string) bool {
			return slices.ContainsFunc(optedOut, func(prefix string) bool {
				return path == prefix || strings.HasPrefix(path, prefix+" ")
			})
		}

		var leaves []*cobra.Command
		cli.WalkCommands(cli.NewRoot(ctx, h.app()), func(cmd *cobra.Command) {
			if cmd.Runnable() && !cmd.HasSubCommands() {
				leaves = append(leaves, cmd)
			}
		})
		Expect(leaves).NotTo(BeEmpty())

		for _, leaf := range leaves {
			path := leaf.CommandPath()
			before := readIfExists(logPath)
			code := run(ctx, h.app(append(strings.Fields(strings.TrimPrefix(path, "coroner ")), "--no-input")...))

			if isOptedOut(path) {
				Expect(readIfExists(logPath)).To(Equal(before), path)
				continue
			}
			if !openedTheDB {
				continue
			}
			last := lastLogLine(logPath)
			Expect(last).To(HaveKeyWithValue("msg", "command finished"), path)
			Expect(last).To(HaveKeyWithValue("command", path), path)
			Expect(last).To(HaveKeyWithValue("exit_code", BeNumerically("==", code)), path)
		}

		before := readFile(logPath)
		completion := h
		completion.db = ""
		completion.env = map[string]string{"HOME": h.home, "CORONER_DB": h.db}
		run(ctx, completion.app("__complete", "timeline", "use", ""))
		Expect(openedTheDB).To(BeTrue())
		Expect(readFile(logPath)).To(Equal(before))
	})

	It("hands the store the command's logger and clock", func(ctx SpecContext) {
		app := h.app("timeline", "create", "u", "--debug")
		now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
		app.Now = func() time.Time {
			now = now.Add(time.Millisecond)
			return now
		}

		Expect(cli.Run(ctx, app)).To(Equal(0))

		Expect(logLines(logPath)).To(ContainElement(And(HaveKeyWithValue("msg", "transaction committed"),
			HaveKeyWithValue("command", "coroner timeline create"), HaveKeyWithValue("duration", BeNumerically(">", 0)))))
	})

	It("logs a dry run's transaction as rolled back under --debug", func(ctx SpecContext) {
		Expect(cli.Run(ctx, h.app("timeline", "create", "inc"))).To(Equal(0))

		Expect(cli.Run(ctx, h.app("event", "add", "x", "-t", "inc", "--dry-run", "--debug"))).To(Equal(0))

		Expect(logLines(logPath)).To(ContainElement(And(HaveKeyWithValue("msg", "transaction rolled back"),
			HaveKeyWithValue("command", "coroner event add"), HaveKeyWithValue("dry_run", true))))
	})

	DescribeTable("ends the log with the command's finish line",
		func(ctx SpecContext, wantCode int, wantKind string, args ...string) {
			Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))

			Expect(run(ctx, h.app(args...))).To(Equal(wantCode))

			last := lastLogLine(logPath)
			Expect(last).To(HaveKeyWithValue("msg", "command finished"))
			Expect(last).To(HaveKeyWithValue("level", "INFO"))
			Expect(last).To(HaveKeyWithValue("kind", wantKind))
			Expect(last).To(HaveKeyWithValue("command", "coroner "+strings.Join(args[:2], " ")))
		},
		Entry(nil, 0, "ok", "timeline", "create", "u"),
		Entry(nil, 0, "ok", "timeline", "list"),
		Entry(nil, 3, "timeline_not_found", "event", "list", "-t", "missing"),
	)

	It("ends the log with status's finish line", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))

		Expect(run(ctx, h.app("status"))).To(Equal(0))

		Expect(lastLogLine(logPath)).To(HaveKeyWithValue("command", "coroner status"))
	})

	DescribeTable("leaves nothing where a database that does not exist would be",
		func(ctx SpecContext, args func(dir string) []string) {
			dir := filepath.Join(h.home, "absent")
			h.db = filepath.Join(dir, "coroner.db")

			run(ctx, h.app(args(h.home)...))

			Expect(dir).NotTo(BeAnExistingFile())
		},
		Entry("status", func(string) []string { return []string{"status"} }),
		Entry("import --dry-run", func(home string) []string {
			file := filepath.Join(home, "in.jsonl")
			Expect(os.WriteFile(file, nil, 0o600)).To(Succeed())
			return []string{"import", "--dry-run", file}
		}),
	)

	DescribeTable("keeps no log beside a path that is not a coroner database",
		func(ctx SpecContext, makeDB func(path string)) {
			h.db = filepath.Join(h.home, "incidents")
			makeDB(h.db)

			Expect(run(ctx, h.app("timeline", "list"))).To(Equal(5))

			Expect(config.DiagnosticLogPath(h.db)).NotTo(BeAnExistingFile())
		},
		Entry("a directory", func(path string) { Expect(os.Mkdir(path, 0o700)).To(Succeed()) }),
		Entry("a file coroner did not create", func(path string) {
			Expect(os.WriteFile(path, []byte("not a database"), 0o600)).To(Succeed())
		}),
	)

	It("finishes a command that panicked after opening the database as internal", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		h.out = panickingWriter{}

		Expect(run(ctx, h.app("timeline", "list"))).To(Equal(1))

		Expect(lastLogLine(logPath)).To(HaveKeyWithValue("kind", "internal"))
	})

	DescribeTable("changes nothing about a command when the log cannot be written",
		func(ctx SpecContext, logSize int64, breakLog func(app *cli.App)) {
			Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
			out.Reset()
			errOut.Reset()
			wantCode := run(ctx, h.app("timeline", "list"))
			wantOut, wantErr := out.String(), errOut.String()
			Expect(os.Remove(logPath)).To(Succeed())
			if logSize > 0 {
				Expect(os.WriteFile(logPath, nil, 0o600)).To(Succeed())
				Expect(os.Truncate(logPath, logSize)).To(Succeed())
			}
			before, _ := os.Stat(logPath)
			out.Reset()
			errOut.Reset()

			app := h.app("timeline", "list")
			breakLog(&app)

			Expect(run(ctx, app)).To(Equal(wantCode))
			Expect(out.String()).To(Equal(wantOut))
			Expect(errOut.String()).To(Equal(wantErr))
			after, _ := os.Stat(logPath)
			if before == nil {
				Expect(after).To(BeNil())
			} else {
				Expect(after.Size()).To(Equal(before.Size()))
				Expect(after.ModTime()).To(Equal(before.ModTime()))
			}
			Expect(logPath + ".1").NotTo(BeAnExistingFile())
		},
		Entry("OpenFile fails for the log", int64(0), func(app *cli.App) {
			open := app.OpenFile
			app.OpenFile = func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error) {
				if strings.HasSuffix(name, ".log") {
					return nil, fs.ErrPermission
				}
				return open(name, flag, perm)
			}
		}),
		Entry("Rename fails for a log past the rotation size", int64(cli.LogRotateSize+1), func(app *cli.App) {
			app.Rename = func(string, string) error { return fs.ErrPermission }
		}),
	)

	It("writes byte-identical logs for identical runs", func(ctx SpecContext) {
		other := h
		other.home = GinkgoT().TempDir()
		other.db = filepath.Join(other.home, "coroner.db")
		for _, each := range []harness{h, other} {
			Expect(run(ctx, each.app("timeline", "create", "t"))).To(Equal(0))
			Expect(run(ctx, each.app("timeline", "list"))).To(Equal(0))
		}

		Expect(readFile(logPath)).To(Equal(readFile(config.DiagnosticLogPath(other.db))))
		Expect(readFile(logPath)).To(ContainSubstring(`"duration":0`))
	})

	It("holds only info lines without debug, and prints none on stderr", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		Expect(run(ctx, h.app("event", "list", "-t", "missing"))).To(Equal(3))

		for _, line := range logLines(logPath) {
			Expect(line).To(HaveKeyWithValue("level", "INFO"))
		}
		Expect(errOut.String()).NotTo(ContainSubstring("level="))
	})

	It("logs where the database path, config file and default time zone came from under --debug", func(ctx SpecContext) {
		h.config = "default_timezone = \"America/New_York\"\n"

		Expect(run(ctx, h.app("timeline", "create", "u", "--debug"))).To(Equal(0))

		lines := logLines(logPath)
		finished := slices.IndexFunc(lines, func(line map[string]any) bool { return line["msg"] == "command finished" })
		Expect(finished).To(BeNumerically(">=", 0))
		linesWith := func(msg string) []map[string]any {
			return slices.DeleteFunc(slices.Clone(lines[:finished]), func(line map[string]any) bool { return line["msg"] != msg })
		}
		Expect(linesWith("database path resolved")).To(HaveExactElements(And(HaveKeyWithValue("level", "DEBUG"),
			HaveKeyWithValue("path", h.db), HaveKeyWithValue("by", "flag"))))
		Expect(linesWith("config file resolved")).To(HaveExactElements(And(HaveKeyWithValue("level", "DEBUG"),
			HaveKeyWithValue("path", filepath.Join(h.home, ".config", "coroner", "config.toml")),
			HaveKeyWithValue("by", "default"))))
		Expect(linesWith("default time zone resolved")).To(HaveExactElements(And(HaveKeyWithValue("level", "DEBUG"),
			HaveKeyWithValue("zone", "America/New_York"), HaveKeyWithValue("by", "file"))))
	})

	It("logs how many lines an imported export held and its format versions under --debug", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "inc"))).To(Equal(0))
		Expect(run(ctx, h.app("event", "add", "x", "-t", "inc"))).To(Equal(0))
		path := filepath.Join(h.home, "inc.jsonl")
		Expect(run(ctx, h.app("timeline", "export", "-t", "inc", "--format", "jsonl", "-o", path))).To(Equal(0))
		data := readFile(path)
		first, _, _ := strings.Cut(data, "\n")
		var header struct {
			FormatVersion string `json:"format_version"`
		}
		Expect(json.Unmarshal([]byte(first), &header)).To(Succeed())

		Expect(run(ctx, h.app("import", path, "--debug"))).To(Equal(0), errOut.String())

		decoded := slices.DeleteFunc(logLines(logPath), func(line map[string]any) bool { return line["msg"] != "export decoded" })
		Expect(decoded).To(HaveExactElements(And(HaveKeyWithValue("level", "DEBUG"), HaveKeyWithValue("path", path),
			HaveKeyWithValue("lines", float64(strings.Count(data, "\n"))),
			HaveKeyWithValue("format_versions", HaveExactElements(header.FormatVersion)))))
	})

	It("counts what an import inserts, skips and replaces at info, and lists each record under --debug", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "inc"))).To(Equal(0))
		Expect(run(ctx, h.app("event", "add", "a", "-t", "inc"))).To(Equal(0))
		Expect(run(ctx, h.app("event", "add", "b", "-t", "inc"))).To(Equal(0))
		type withID struct {
			ID string `json:"id"`
		}
		var listed struct {
			Timelines []withID `json:"timelines"`
			Events    []withID `json:"events"`
		}
		out.Reset()
		Expect(run(ctx, h.app("event", "list", "-t", "inc", "--json"))).To(Equal(0))
		Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed())
		timelineID, eventA, eventB := listed.Timelines[0].ID, listed.Events[0].ID, listed.Events[1].ID

		path := filepath.Join(h.home, "inc.jsonl")
		Expect(run(ctx, h.app("timeline", "export", "-t", "inc", "--format", "jsonl", "-o", path))).To(Equal(0))
		Expect(run(ctx, h.app("timeline", "edit", "inc", "--title", "changed"))).To(Equal(0))
		Expect(run(ctx, h.app("event", "delete", "inc#2"))).To(Equal(0))

		// written runs args and returns the log lines it wrote.
		written := func(want int, args ...string) []map[string]any {
			before := len(logLines(logPath))
			Expect(run(ctx, h.app(args...))).To(Equal(want), errOut.String())
			return logLines(logPath)[before:]
		}
		isPlanLine := func(line map[string]any) bool {
			msg, _ := line["msg"].(string)
			return strings.HasSuffix(msg, " inserted") || strings.HasSuffix(msg, " skipped") || strings.HasSuffix(msg, " replaced")
		}
		line := func(level, msg string, keys ...any) types.GomegaMatcher {
			matchers := []types.GomegaMatcher{HaveKeyWithValue("level", level), HaveKeyWithValue("msg", msg)}
			for i := 0; i < len(keys); i += 2 {
				matchers = append(matchers, HaveKeyWithValue(keys[i], keys[i+1]))
			}
			return And(matchers...)
		}

		Expect(written(0, "import", path, "--on-conflict", "replace", "--dry-run", "--debug")).To(ContainElement(
			line("INFO", "events inserted", "count", float64(1), "dry_run", true)))

		lines := written(0, "import", path, "--on-conflict", "replace", "--debug")
		Expect(lines).To(ContainElements(
			line("INFO", "timelines replaced", "count", float64(1)),
			line("INFO", "events skipped", "count", float64(1)),
			line("INFO", "events inserted", "count", float64(1)),
			line("DEBUG", "timelines replaced", "timeline_id", timelineID),
			line("DEBUG", "events skipped", "event", eventA),
			line("DEBUG", "events inserted", "event", eventB),
		))
		Expect(slices.ContainsFunc(lines, func(l map[string]any) bool { _, ok := l["dry_run"]; return isPlanLine(l) && ok })).To(BeFalse())

		Expect(run(ctx, h.app("timeline", "edit", "inc", "--title", "changed-again"))).To(Equal(0))
		Expect(slices.ContainsFunc(written(4, "import", path, "--debug"), isPlanLine)).To(BeFalse())

		var events strings.Builder
		for l := range strings.Lines(readFile(path)) {
			if strings.Contains(l, `"type":"event"`) {
				events.WriteString(l)
			}
		}
		eventsPath := filepath.Join(h.home, "events.jsonl")
		Expect(os.WriteFile(eventsPath, []byte(events.String()), 0o600)).To(Succeed())
		Expect(written(0, "event", "add", "-t", "inc", "--from-file", eventsPath, "--debug")).To(ContainElements(
			line("INFO", "events skipped", "count", float64(2)),
			line("DEBUG", "events skipped", "event", eventA),
			line("DEBUG", "events skipped", "event", eventB),
		))
	})

	DescribeTable("logs which setting selected the timelines under --debug",
		func(ctx SpecContext, wantBy string, wantNames []string, setup func(context.Context), args ...string) {
			Expect(run(ctx, h.app("timeline", "create", "inc"))).To(Equal(0))
			setup(ctx)
			out.Reset()
			Expect(run(ctx, h.app("timeline", "list", "--json"))).To(Equal(0))
			var listed struct {
				Timelines []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"timelines"`
			}
			Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed(), out.String())
			idOf := map[string]any{}
			for _, t := range listed.Timelines {
				idOf[t.Name] = t.ID
			}
			var want []any
			for _, name := range wantNames {
				want = append(want, idOf[name])
			}

			Expect(run(ctx, h.app(append(args, "--debug")...))).To(Equal(0), errOut.String())

			Expect(logLines(logPath)).To(ContainElement(And(
				HaveKeyWithValue("level", "DEBUG"), HaveKeyWithValue("msg", "timelines selected"),
				HaveKeyWithValue("by", wantBy), HaveKeyWithValue("timeline_ids", HaveExactElements(want...)),
			)))
		},
		Entry("-t", "flag", []string{"inc"}, func(context.Context) {}, "event", "list", "-t", "inc"),
		Entry("--overlay", "overlay", []string{"standby", "inc"}, func(ctx context.Context) {
			Expect(run(ctx, h.app("timeline", "create", "standby"))).To(Equal(0))
			Expect(run(ctx, h.app("overlay", "create", "pair", "-t", "standby", "-t", "inc"))).To(Equal(0), errOut.String())
		}, "event", "list", "--overlay", "pair"),
		Entry("CORONER_TIMELINE", "env", []string{"inc"}, func(context.Context) {
			h.env = map[string]string{"HOME": h.home, "CORONER_TIMELINE": "inc"}
		}, "event", "list"),
		Entry("coroner timeline use", "use", []string{"inc"}, func(ctx context.Context) {
			Expect(run(ctx, h.app("timeline", "use", "inc"))).To(Equal(0))
		}, "event", "list"),
	)

	It("logs each typed time under --debug", func(ctx SpecContext) {
		resolvedLines := func() []map[string]any {
			var found []map[string]any
			for _, line := range logLines(logPath) {
				if line["msg"] == "time input resolved" {
					Expect(line).To(HaveKeyWithValue("level", "DEBUG"))
					found = append(found, line)
				}
			}
			return found
		}
		instant := func(value string) time.Time {
			GinkgoHelper()
			t, err := time.Parse(time.RFC3339Nano, value)
			Expect(err).NotTo(HaveOccurred())
			return t
		}
		expectLine := func(line map[string]any, flag, input, zone, want, precision string) {
			GinkgoHelper()
			Expect(line).To(HaveKeyWithValue("flag", flag))
			Expect(line).To(HaveKeyWithValue("input", input))
			Expect(line).To(HaveKeyWithValue("zone", zone))
			Expect(line).To(HaveKeyWithValue("precision", precision))
			logged, ok := line["instant"].(string)
			Expect(ok).To(BeTrue(), line["instant"])
			Expect(instant(logged).Equal(instant(want))).To(BeTrue(), logged)
		}
		Expect(run(ctx, h.app("timeline", "create", "inc"))).To(Equal(0))

		Expect(run(ctx, h.app("event", "add", "x", "-t", "inc", "--at", "2026-01-02 03:04",
			"--ended", "2026-01-02 04:00", "--tz", "America/New_York", "--debug"))).To(Equal(0), errOut.String())
		lines := resolvedLines()
		Expect(lines).To(HaveLen(2))
		expectLine(lines[0], "at", "2026-01-02 03:04", "America/New_York", "2026-01-02T03:04:00-05:00", "minute")
		expectLine(lines[1], "ended", "2026-01-02 04:00", "America/New_York", "2026-01-02T04:00:00-05:00", "minute")

		Expect(run(ctx, h.app("event", "list", "-t", "inc", "--since", "2026-01-01", "--debug"))).To(Equal(0), errOut.String())
		lines = resolvedLines()
		Expect(lines).To(HaveLen(3))
		expectLine(lines[2], "since", "2026-01-01", "UTC", "2026-01-01T00:00:00Z", "day")

		Expect(run(ctx, h.app("event", "add", "y", "-t", "inc", "--debug"))).To(Equal(0), errOut.String())
		Expect(resolvedLines()).To(HaveLen(3))
	})

	It("logs the failure under --debug, and shows every line on stderr once", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		errOut.Reset()

		Expect(run(ctx, h.app("event", "list", "-t", "missing", "--debug"))).To(Equal(3))

		Expect(slices.ContainsFunc(logLines(logPath), func(line map[string]any) bool {
			return line["msg"] == "command failed" && line["level"] == "DEBUG"
		})).To(BeTrue())
		Expect(strings.Count(errOut.String(), `msg="command failed"`)).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring(`level=INFO msg="command finished"`))
	})

	It("keeps the JSON error envelope last on stderr under --json --debug", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		errOut.Reset()

		Expect(run(ctx, h.app("event", "list", "-t", "missing", "--json", "--debug"))).To(Equal(3))

		envelope, err := lastEnvelope(errOut.String())
		Expect(err).NotTo(HaveOccurred())
		Expect(envelope).To(HaveKeyWithValue("kind", "timeline_not_found"))
	})

	It("prints debug text from a root the deprecation bridge builds", func(ctx SpecContext) {
		app := h.app("--debug", "--no-such-flag")

		Expect(func() { cli.RunWithThrowawayDeprecation(ctx, app) }).NotTo(Panic())
		Expect(errOut.String()).To(ContainSubstring("level=DEBUG"))
	})

	Describe("files written", func() {
		var attachmentID, attachmentSHA256 string

		BeforeEach(func(ctx SpecContext) {
			file := filepath.Join(h.home, "shot.txt")
			Expect(os.WriteFile(file, []byte("shot"), 0o600)).To(Succeed())
			Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
			Expect(run(ctx, h.app("event", "add", "x", "-t", "t", "--attach", file))).To(Equal(0))
			out.Reset()
			Expect(run(ctx, h.app("attachment", "list", "-t", "t", "--json"))).To(Equal(0))
			var listed struct {
				Attachments []struct {
					ID     string `json:"id"`
					SHA256 string `json:"sha256"`
				} `json:"attachments"`
			}
			Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed(), out.String())
			Expect(listed.Attachments).To(HaveLen(1))
			attachmentID, attachmentSHA256 = listed.Attachments[0].ID, listed.Attachments[0].SHA256
		})

		// written runs args and returns the file written lines it logged.
		written := func(ctx context.Context, args ...string) []map[string]any {
			GinkgoHelper()
			before := len(readIfExists(logPath))
			Expect(run(ctx, h.app(args...))).To(Equal(0), errOut.String())
			var lines []map[string]any
			for _, line := range decodeLines(readFile(logPath)[before:]) {
				if line["msg"] == "file written" {
					lines = append(lines, line)
				}
			}
			return lines
		}

		sizeOf := func(path string) float64 {
			GinkgoHelper()
			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			return float64(info.Size())
		}

		It("logs the export and each attachment it copies into a directory", func(ctx SpecContext) {
			dir := filepath.Join(h.home, "out")

			lines := written(ctx, "timeline", "export", "-t", "t", "--format", "md", "-o", dir+"/")

			exported := filepath.Join(dir, "t.md")
			Expect(lines).To(ContainElement(SatisfyAll(
				HaveKeyWithValue("level", "INFO"),
				HaveKeyWithValue("path", exported),
				HaveKeyWithValue("size", sizeOf(exported)),
				HaveKeyWithValue("format", "md"),
				HaveKeyWithValue("counts", SatisfyAll(
					HaveKeyWithValue("events", BeNumerically("==", 1)),
					HaveKeyWithValue("attachments", BeNumerically("==", 1)),
				)),
			)))
			copied, err := filepath.Glob(filepath.Join(dir, "attachments", "*"))
			Expect(err).NotTo(HaveOccurred())
			Expect(copied).To(HaveLen(1))
			Expect(lines).To(ContainElement(SatisfyAll(
				HaveKeyWithValue("level", "INFO"),
				HaveKeyWithValue("path", copied[0]),
				HaveKeyWithValue("size", sizeOf(copied[0])),
				HaveKeyWithValue("attachment", attachmentID),
				HaveKeyWithValue("sha256", attachmentSHA256),
			)))
		})

		It("logs the file attachment export writes", func(ctx SpecContext) {
			file := filepath.Join(h.home, "exported.txt")

			Expect(written(ctx, "attachment", "export", attachmentID, "-o", file)).To(ConsistOf(SatisfyAll(
				HaveKeyWithValue("level", "INFO"),
				HaveKeyWithValue("path", file),
				HaveKeyWithValue("size", sizeOf(file)),
				HaveKeyWithValue("attachment", attachmentID),
				HaveKeyWithValue("sha256", attachmentSHA256),
			)))
		})

		It("logs no file written for output to stdout", func(ctx SpecContext) {
			Expect(written(ctx, "timeline", "export", "-t", "t", "--format", "jsonl")).To(BeEmpty())
			Expect(written(ctx, "attachment", "export", attachmentID, "-o", "-")).To(BeEmpty())
		})
	})
})
