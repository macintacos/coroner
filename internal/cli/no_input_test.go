package cli_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

// noInputScript is the script that runs each interactive command with no terminal.
const noInputScript = "testdata/script/no-input.txtar"

var _ = Describe("interactive commands off a terminal", func() {
	It("each have a run in "+noInputScript, func(ctx SpecContext) {
		script, err := os.ReadFile(noInputScript)
		Expect(err).NotTo(HaveOccurred())

		var missing []string
		for _, cmd := range interactiveCommands(cli.NewRoot(ctx, cli.App{})) {
			if !noTerminalRun(cmd).Match(script) {
				missing = append(missing, cmd.CommandPath())
			}
		}

		Expect(missing).To(BeEmpty(), "run each of these with no terminal in %s", noInputScript)
	})

	DescribeTable("exit 2 at once, never waiting on a stdin that stays open, when the command line gives no input",
		func(ctx SpecContext, names string, args ...string) {
			home := GinkgoT().TempDir()
			stdin, typed := io.Pipe()
			DeferCleanup(typed.Close)
			var errOut bytes.Buffer
			app := cli.App{
				Args: append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
				In:   stdin, Out: io.Discard, Err: &errOut, OutIsTTY: true,
				LookupEnv: func(key string) (string, bool) {
					value, ok := map[string]string{"HOME": home}[key]
					return value, ok
				},
				ReadFile:  readNoFile,
				Now:       time.Now,
				NewID:     uuid.NewV7,
				OpenStore: store.Open,
			}

			code := make(chan int, 1)
			go func() { code <- cli.Run(ctx, app) }()

			Eventually(code).Within(100 * time.Millisecond).Should(Receive(Equal(2)))
			Expect(errOut.String()).To(ContainSubstring(names))
		},
		Entry("event add", "- to read it from stdin", "event", "add"),
		Entry("annotation add", "--kind", "annotation", "add"),
		Entry("timeline create", "coroner timeline create --help", "timeline", "create"),
		Entry("timeline edit", "--name", "timeline", "edit", "mongo-outage"),
	)
})

// interactiveCommands returns each command that can open a form, picker or viewer: those
// with --interactive, and the viewer's and picker's commands, which have no flag to find them
// by. A new command that opens a picker or viewer without --interactive must be added to its
// list.
func interactiveCommands(root *cobra.Command) []*cobra.Command {
	GinkgoHelper()
	var cmds []*cobra.Command
	for _, path := range []string{"timeline view", "timeline list"} {
		cmd, _, err := root.Find(strings.Fields(path))
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.CommandPath()).To(Equal("coroner " + path))
		cmds = append(cmds, cmd)
	}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if hasInteractive(cmd) {
			cmds = append(cmds, cmd)
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	return cmds
}

// noTerminalRun matches a line of noInputScript that runs cmd. A form command's run must be
// the exit 2 it falls back to, since a flags-only run passes even when the fallback is gone.
func noTerminalRun(cmd *cobra.Command) *regexp.Regexp {
	run := `(exits \d+|!? *exec)`
	if hasInteractive(cmd) {
		run = `exits 2`
	}
	return regexp.MustCompile(`(?m)^` + run + ` ` + regexp.QuoteMeta(cmd.CommandPath()) + `( |$)`)
}

func hasInteractive(cmd *cobra.Command) bool {
	return cmd.LocalFlags().Lookup("interactive") != nil
}
