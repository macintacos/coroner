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
		for _, path := range interactiveCommands(cli.NewRoot(ctx, cli.App{})) {
			ran := regexp.MustCompile(`(?m)^(exits \d+|!? *exec) coroner ` + regexp.QuoteMeta(path) + `( |$)`)
			if !ran.Match(script) {
				missing = append(missing, path)
			}
		}

		Expect(missing).To(BeEmpty(), "run each of these with no terminal in %s", noInputScript)
	})

	DescribeTable("exit 2 at once, never waiting on a stdin that stays open, when the command line gives no input",
		func(ctx SpecContext, names string, args ...string) {
			home := GinkgoT().TempDir()
			stdin, _ := io.Pipe()
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

// interactiveCommands returns the path below coroner of each command that can open a form,
// picker or viewer: those with --interactive, and the viewer's and picker's commands, which
// have no flag to find them by.
func interactiveCommands(root *cobra.Command) []string {
	GinkgoHelper()
	paths := []string{"timeline view", "timeline list"}
	for _, path := range paths {
		cmd, _, err := root.Find(strings.Fields(path))
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.CommandPath()).To(Equal("coroner " + path))
	}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.LocalFlags().Lookup("interactive") != nil {
			paths = append(paths, strings.TrimPrefix(cmd.CommandPath(), "coroner "))
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	return paths
}
