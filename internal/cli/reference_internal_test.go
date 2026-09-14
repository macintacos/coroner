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

	render := func(root *cobra.Command, topics ...HelpTopic) string {
		GinkgoHelper()
		var out strings.Builder
		Expect(writeReference(&out, root, topics)).To(Succeed())

		return out.String()
	}

	It("writes each command's description, usage, subcommands, examples and flags under its path, and root's help topics", func() {
		root := &cobra.Command{Use: "tool", Short: "A tool", Run: noop}
		root.PersistentFlags().String("db", "", "Database `path` (env: TOOL_DB)")
		root.PersistentFlags().BoolP("help", "h", false, "Show help")
		add := &cobra.Command{
			Use: "add <text>", Short: "Add a thing", Long: "Add a thing to the list.", Example: "  $ tool add milk", Run: noop,
		}
		add.Flags().Bool("dry-run", false, "Add nothing")
		root.AddCommand(add)

		Expect(render(root, HelpTopic{Name: "time", Short: "Typing times"})).To(Equal(`# tool

~~~text
A tool
~~~

~~~text
tool [flags]
~~~

Commands:

~~~text
add          Add a thing
~~~

Help topics:

~~~text
time         Typing times
~~~

Global flags:

~~~text
      --db path   Database path (env: TOOL_DB)
  -h, --help      Show help
~~~

## tool add

~~~text
Add a thing to the list.
~~~

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

		Expect(markdownHeadings(page)).To(Equal([]string{
			"# tool", "## tool completion", "## tool event", "## tool event add", "## tool event list", "## tool version",
		}))
		Expect(page).To(MatchRegexp(`(?m)^help +Help about any command$`))
		Expect(page).To(MatchRegexp(`(?m)^bash +Generate the autocompletion script for bash$`))
		Expect(page).NotTo(ContainSubstring("secret"))
		Expect(page).NotTo(ContainSubstring("legacy"))
	})

	It("gives every available coroner command a section, and help and the completion shells a listed line", func(ctx SpecContext) {
		root := NewRoot(ctx, App{})
		page := render(root)

		expected := []string{"# coroner"}
		var listedOnly []*cobra.Command
		var visit func(*cobra.Command)
		visit = func(cmd *cobra.Command) {
			for _, child := range cmd.Commands() {
				switch {
				case child.Name() == "help" || cmd.Name() == "completion":
					listedOnly = append(listedOnly, child)
				case child.IsAvailableCommand():
					expected = append(expected, "## "+child.CommandPath())
					visit(child)
				}
			}
		}
		visit(root)

		Expect(markdownHeadings(page)).To(Equal(expected))
		Expect(listedOnly).NotTo(BeEmpty())
		for _, cmd := range listedOnly {
			Expect(page).To(MatchRegexp(`(?m)^`+regexp.QuoteMeta(cmd.Name())+` +`+regexp.QuoteMeta(cmd.Short)+`$`), cmd.CommandPath())
		}
	})

	It("writes no Markdown that renders as HTML, so placeholders such as <id> survive", func(ctx SpecContext) {
		var html []string
		walkMarkdown(render(NewRoot(ctx, App{})), func(n ast.Node, source []byte) {
			switch n := n.(type) {
			case *ast.RawHTML:
				html = append(html, string(n.Segments.Value(source)))
			case *ast.HTMLBlock:
				html = append(html, string(n.Lines().Value(source)))
			}
		})

		Expect(html).To(BeEmpty())
	})

	It("writes Markdown that passes rumdl check", func(ctx SpecContext) {
		_, err := exec.LookPath("rumdl")
		Expect(err).NotTo(HaveOccurred(), "rumdl is not on PATH: run the suite through mise run test")

		cmd := exec.CommandContext(ctx, "rumdl", "check", "-")
		cmd.Stdin = strings.NewReader(render(NewRoot(ctx, App{})))
		out, err := cmd.CombinedOutput()

		Expect(err).NotTo(HaveOccurred(), string(out))
	})
})

// markdownHeadings returns page's headings as goldmark parses them, each as its # markers and
// text.
func markdownHeadings(page string) []string {
	var found []string
	walkMarkdown(page, func(n ast.Node, source []byte) {
		if heading, ok := n.(*ast.Heading); ok {
			found = append(found, strings.Repeat("#", heading.Level)+" "+string(heading.Lines().Value(source)))
		}
	})

	return found
}

// walkMarkdown calls visit with each node of page as goldmark parses it, parents first.
func walkMarkdown(page string, visit func(n ast.Node, source []byte)) {
	GinkgoHelper()
	source := []byte(page)

	Expect(ast.Walk(goldmark.New().Parser().Parse(text.NewReader(source)), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			visit(n, source)
		}

		return ast.WalkContinue, nil
	})).To(Succeed())
}
