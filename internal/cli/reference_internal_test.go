package cli

import (
	"os/exec"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var _ = Describe("writeReference", func() {
	noop := func(*cobra.Command, []string) {}

	render := func(root *cobra.Command) string {
		GinkgoHelper()
		var out strings.Builder
		Expect(writeReference(&out, root)).To(Succeed())

		return out.String()
	}

	It("writes each command's description, usage, subcommands, examples and flags under its path", func() {
		root := &cobra.Command{Use: "tool", Short: "A tool", Run: noop}
		root.PersistentFlags().String("db", "", "Database `path` (env: TOOL_DB)")
		root.PersistentFlags().BoolP("help", "h", false, "Show help")
		add := &cobra.Command{
			Use: "add <text>", Short: "Add a thing", Long: "Add a thing to the list.", Example: "  $ tool add milk", Run: noop,
		}
		add.Flags().Bool("dry-run", false, "Add nothing")
		root.AddCommand(add)

		Expect(render(root)).To(Equal(`# tool

A tool

~~~text
tool [flags]
~~~

Commands:

~~~text
add          Add a thing
~~~

Global flags:

~~~text
      --db path   Database path (env: TOOL_DB)
  -h, --help      Show help
~~~

## tool add

Add a thing to the list.

~~~text
tool add <text> [flags]
~~~

Examples:

~~~console
  $ tool add milk
~~~

Flags:

~~~text
      --dry-run   Add nothing
~~~

Global flags:

~~~text
      --db path   Database path (env: TOOL_DB)
  -h, --help      Show help
~~~
`))
	})

	It("gives a section to each command help lists, depth-first, and help and the completion shells one line each", func() {
		root := &cobra.Command{Use: "tool"}
		event := &cobra.Command{Use: "event", Short: "Manage events"}
		event.AddCommand(
			&cobra.Command{Use: "list", Short: "List events", Run: noop},
			&cobra.Command{Use: "add", Short: "Add an event", Run: noop},
		)
		root.AddCommand(event,
			&cobra.Command{Use: "version", Short: "Show the version", Run: noop},
			&cobra.Command{Use: "secret", Short: "Hidden", Hidden: true, Run: noop},
			&cobra.Command{Use: "legacy", Short: "Deprecated", Deprecated: "use version", Run: noop},
		)
		root.InitDefaultHelpCmd()
		root.InitDefaultCompletionCmd()

		page := render(root)

		Expect(headings(page)).To(Equal([]string{
			"# tool", "## tool completion", "## tool event", "## tool event add", "## tool event list", "## tool version",
		}))
		Expect(page).To(MatchRegexp(`(?m)^help +Help about any command$`))
		Expect(page).To(MatchRegexp(`(?m)^bash +Generate the autocompletion script for bash$`))
		Expect(page).NotTo(ContainSubstring("secret"))
		Expect(page).NotTo(ContainSubstring("legacy"))
	})

	It("names every coroner command that is neither hidden nor deprecated, as a heading or a listed line", func(ctx SpecContext) {
		root := NewRoot(ctx, App{})
		page := render(root)

		var missing []string
		var visit func(*cobra.Command)
		visit = func(cmd *cobra.Command) {
			if cmd.Hidden || cmd.Deprecated != "" {
				return
			}

			named := regexp.MustCompile(`(?m)^(#{1,2} ` + regexp.QuoteMeta(cmd.CommandPath()) + `|` +
				regexp.QuoteMeta(cmd.Name()) + ` +` + regexp.QuoteMeta(cmd.Short) + `)$`)
			if !named.MatchString(page) {
				missing = append(missing, cmd.CommandPath())
			}
			for _, child := range cmd.Commands() {
				visit(child)
			}
		}
		visit(root)

		Expect(missing).To(BeEmpty())
	})

	It("writes Markdown that passes rumdl check, line length aside", func(ctx SpecContext) {
		_, err := exec.LookPath("rumdl")
		Expect(err).NotTo(HaveOccurred(), "rumdl is not on PATH: run the suite through mise run test")

		cmd := exec.CommandContext(ctx, "rumdl", "check", "--extend-disable", "MD013", "-")
		cmd.Stdin = strings.NewReader(render(NewRoot(ctx, App{})))
		out, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred(), string(out))
	})
})

// headings returns page's headings as goldmark parses them, each as its # markers and text.
func headings(page string) []string {
	GinkgoHelper()
	source := []byte(page)
	var found []string

	Expect(ast.Walk(goldmark.New().Parser().Parse(text.NewReader(source)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if heading, ok := n.(*ast.Heading); ok && entering {
			line := heading.Lines().At(0)
			found = append(found, strings.Repeat("#", heading.Level)+" "+string(line.Value(source)))
		}

		return ast.WalkContinue, nil
	})).To(Succeed())

	return found
}
