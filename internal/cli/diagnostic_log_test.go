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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// logLines returns the diagnostic log at path, one decoded JSON object per line.
func logLines(path string) []map[string]any {
	GinkgoHelper()
	var lines []map[string]any
	for line := range strings.Lines(readFile(path)) {
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
		h            harness
		logPath      string
		out, errOut  bytes.Buffer
		openedTheLog bool
	)

	BeforeEach(func() {
		out.Reset()
		errOut.Reset()
		dir := GinkgoT().TempDir()
		ids := 0
		h = harness{home: dir, db: filepath.Join(dir, "coroner.db"), out: &out, errOut: &errOut, newID: countedIDs(&ids)}
		logPath = config.LogPath(h.db)
	})

	// run runs args against h, recording whether the command opened the database.
	run := func(ctx context.Context, app cli.App) int {
		openedTheLog = false
		openStore, openReadOnly := app.OpenStore, app.OpenStoreReadOnly
		app.OpenStore = func(ctx context.Context, path string) (*store.Store, error) {
			openedTheLog = true
			return openStore(ctx, path)
		}
		app.OpenStoreReadOnly = func(ctx context.Context, path string) (*store.Store, error) {
			openedTheLog = true
			return openReadOnly(ctx, path)
		}
		return cli.Run(ctx, app)
	}

	It("ends every command that opens a database with its finish line, and keeps opted-out commands out", func(ctx SpecContext) {
		Expect(run(ctx, h.app("timeline", "create", "t"))).To(Equal(0))
		Expect(run(ctx, h.app("timeline", "use", "t"))).To(Equal(0))

		optedOut := []string{"coroner version", "coroner agent skill", "coroner completion"}
		isOptedOut := func(path string) bool {
			return slices.ContainsFunc(optedOut, func(prefix string) bool {
				return path == prefix || strings.HasPrefix(path, prefix+" ")
			})
		}

		var leaves []*cobra.Command
		var walk func(cmd *cobra.Command)
		walk = func(cmd *cobra.Command) {
			if cmd.Runnable() && !cmd.HasSubCommands() {
				leaves = append(leaves, cmd)
			}
			for _, child := range cmd.Commands() {
				walk(child)
			}
		}
		walk(cli.NewRoot(ctx, h.app()))
		Expect(leaves).NotTo(BeEmpty())

		for _, leaf := range leaves {
			path := leaf.CommandPath()
			before := readIfExists(logPath)
			code := run(ctx, h.app(append(strings.Fields(strings.TrimPrefix(path, "coroner ")), "--no-input")...))

			if isOptedOut(path) {
				Expect(readIfExists(logPath)).To(Equal(before), path)
				continue
			}
			if !openedTheLog {
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
		Expect(openedTheLog).To(BeTrue())
		Expect(readFile(logPath)).To(Equal(before))
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
			before := readIfExists(logPath)
			out.Reset()
			errOut.Reset()

			app := h.app("timeline", "list")
			breakLog(&app)

			Expect(run(ctx, app)).To(Equal(wantCode))
			Expect(out.String()).To(Equal(wantOut))
			Expect(errOut.String()).To(Equal(wantErr))
			Expect(readIfExists(logPath)).To(Equal(before))
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
		Entry("Rename fails for a log past 50MB", int64(50<<20+1), func(app *cli.App) {
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

		Expect(readFile(logPath)).To(Equal(readFile(config.LogPath(other.db))))
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

	It("prints debug text from a root Run never set up", func(ctx SpecContext) {
		app := h.app("--debug", "--no-such-flag")

		Expect(func() { cli.RunWithThrowawayDeprecation(ctx, app) }).NotTo(Panic())
		Expect(errOut.String()).To(ContainSubstring("level=DEBUG"))
	})
})
