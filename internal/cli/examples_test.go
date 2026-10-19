package cli_test

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing/fstest"
	"unicode"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("exampleLines", func() {
	It("returns each line that invokes coroner, from the word coroner, with its line number", func() {
		lines := exampleLines("coroner version", "  $ coroner version\n\n  $ coroner version --json")

		Expect(lines).To(Equal([]exampleLine{
			{Source: "coroner version", Number: 1, Text: "coroner version"},
			{Source: "coroner version", Number: 3, Text: "coroner version --json"},
		}))
	})

	It("starts a piped line at coroner", func() {
		Expect(exampleLines("s", "  $ pbpaste | coroner event add - --source slack")).To(ConsistOf(
			HaveField("Text", "coroner event add - --source slack")))
	})

	It("starts a line at coroner after an environment assignment", func() {
		Expect(exampleLines("s", "  $ CORONER_TIMELINE=x coroner status")).To(ConsistOf(
			HaveField("Text", "coroner status")))
	})

	It("starts a line at coroner after another command", func() {
		Expect(exampleLines("s", "mkdir rca && coroner timeline export --format md -o rca/")).To(ConsistOf(
			HaveField("Text", "coroner timeline export --format md -o rca/")))
	})

	DescribeTable("starts a line at coroner after a shell keyword or a prefix command",
		func(line, text string) {
			Expect(exampleLines("s", line)).To(ConsistOf(SatisfyAll(HaveField("Text", text), HaveField("Skipped", BeEmpty()))))
		},
		Entry("a loop body", `while read -r l; do coroner event add "$l"; done`, `coroner event add "$l"; done`),
		Entry("a condition", "if coroner status; then coroner version; fi", "coroner status; then coroner version; fi"),
		Entry("an else branch", "else coroner version", "coroner version"),
		Entry("sudo", "sudo coroner version", "coroner version"),
		Entry("time", "time coroner version", "coroner version"),
		Entry("env with an assignment", "env CORONER_DB=x.db coroner status", "coroner status"),
		Entry("a negation", "! coroner status", "coroner status"),
	)

	It("ignores comments, and lines where coroner is not a word", func() {
		text := "  # coroner timeline list\n" +
			"  $ cp coroner.fish ~/.config/fish/completions/coroner.fish\n" +
			"  $ ls \"${fpath[1]}/_coroner\""

		Expect(exampleLines("s", text)).To(BeEmpty())
	})

	It("ends a comment at its own line, even after a backslash", func() {
		Expect(exampleLines("s", "  # list them \\\n  $ coroner nosuch")).To(ConsistOf(
			SatisfyAll(HaveField("Number", 2), HaveField("Text", "coroner nosuch"))))
	})

	It("joins a line ending in a backslash to the next, numbered by its first line", func() {
		text := "  $ coroner version\n  $ coroner timeline create x \\\n      --title y"

		Expect(exampleLines("s", text)).To(ContainElement(SatisfyAll(
			HaveField("Number", 2), HaveField("Text", "coroner timeline create x --title y"))))
	})

	DescribeTable("returns a line it cannot check with the reason it is skipped",
		func(line string) {
			Expect(exampleLines("s", line)).To(ConsistOf(HaveField("Skipped", Not(BeEmpty()))))
		},
		Entry("command substitution", "  $ coroner timeline use $(coroner timeline list | head -1)"),
		Entry("a backtick", "  $ coroner timeline use `cat name`"),
		Entry("coroner as an argument", "install -m 0755 coroner ~/.local/bin/coroner"),
		Entry("coroner as a package name", "brew uninstall --cask coroner"),
		Entry("coroner after a command other than a prefix command", "mkdir coroner && cd coroner"),
	)
})

var _ = Describe("markdownExampleLines", func() {
	It("returns the coroner lines of a shell code block, numbered by their line in the page", func() {
		page := "# Check\n\nRun `coroner version` to check.\n\n```sh\ncoroner version\n```\n"

		Expect(markdownExampleLines("how-to/x.md", page)).To(Equal([]exampleLine{
			{Source: "how-to/x.md", Number: 6, Text: "coroner version"},
		}))
	})

	It("returns only the $ lines of a console block and the lines they continue onto", func() {
		page := "```console\n" +
			"$ coroner timeline create x \\\n" +
			"    --title y\n" +
			"Next: coroner timeline use x\n" +
			"      coroner event add -t x <text>\n" +
			"```\n"

		Expect(markdownExampleLines("s", page)).To(ConsistOf(SatisfyAll(
			HaveField("Number", 2), HaveField("Text", "coroner timeline create x --title y"))))
	})

	It("ignores code blocks that are not shell", func() {
		page := "```markdown\ncoroner nosuch\n```\n\n```text\ncoroner nosuch\n```\n\n```\ncoroner nosuch\n```\n"

		Expect(markdownExampleLines("s", page)).To(BeEmpty())
	})

	It("reads a block's language from the first word of its info string", func() {
		page := "```sh title=\"bulk.sh\"\ncoroner version\n```\n"

		Expect(markdownExampleLines("s", page)).To(ConsistOf(HaveField("Text", "coroner version")))
	})

	It("reads a tilde fence, closed by a longer one", func() {
		page := "~~~sh\ncoroner version\n~~~~\n\n```text\ncoroner nosuch\n```\n"

		Expect(markdownExampleLines("s", page)).To(ConsistOf(HaveField("Text", "coroner version")))
	})

	It("closes a block only on a bare fence of its own character at least as long as the opening one", func() {
		page := "````markdown\n" +
			"```sh\ncoroner nosuch\n```\n" +
			"````\n" +
			"````sh\n" +
			"cat > notes.md <<'EOF'\n```text\n~~~~\n```\nEOF\n" +
			"coroner version\n" +
			"````\n"

		Expect(markdownExampleLines("s", page)).To(ConsistOf(SatisfyAll(
			HaveField("Number", 12), HaveField("Text", "coroner version"))))
	})
})

var _ = DescribeTable("splitWords splits a command line as a shell would",
	func(line string, want []string) {
		Expect(splitWords(line)).To(Equal(want))
	},
	Entry("plain words", "coroner timeline list  --json", []string{"coroner", "timeline", "list", "--json"}),
	Entry("a single-quoted argument with spaces", "coroner x --title 'a b'", []string{"coroner", "x", "--title", "a b"}),
	Entry("a double-quoted argument with spaces", `coroner x --title "a b"`, []string{"coroner", "x", "--title", "a b"}),
	Entry("a backslash-escaped quote", `coroner x "say \"hi\"" it\'s`, []string{"coroner", "x", `say "hi"`, "it's"}),
	Entry("an empty quoted argument", `coroner x --title ""`, []string{"coroner", "x", "--title", ""}),
	Entry("words up to a pipe", "coroner completion powershell | Out-String", []string{"coroner", "completion", "powershell"}),
	Entry("words up to a redirection", `coroner completion zsh > "${fpath[1]}/_coroner"`, []string{"coroner", "completion", "zsh"}),
	Entry("a quoted pipe and redirection", `coroner x --title "a | b" 'c > d'`, []string{"coroner", "x", "--title", "a | b", "c > d"}),
	Entry("words up to a comment", "coroner version # prints the version", []string{"coroner", "version"}),
	Entry("a # inside a word", "coroner x a#b", []string{"coroner", "x", "a#b"}),
	Entry("words up to a closing parenthesis", "coroner version) && echo ok", []string{"coroner", "version"}),
	Entry("words up to a redirection of a numbered descriptor", "coroner timeline list --json 2>/dev/null", []string{"coroner", "timeline", "list", "--json"}),
	Entry("words up to a descriptor duplication", "coroner version 2>&1", []string{"coroner", "version"}),
)

var _ = DescribeTable("splitWords rejects an unfinished line",
	func(line string) {
		_, err := splitWords(line)
		Expect(err).To(HaveOccurred())
	},
	Entry("an unterminated double quote", `coroner x --title "a b`),
	Entry("an unterminated single quote", `coroner x --title 'a b`),
	Entry("a trailing backslash", `coroner x \`),
)

var _ = Describe("checkInvocation", func() {
	check := func(ctx SpecContext, line string) error {
		GinkgoHelper()
		args, err := splitWords(line)
		Expect(err).NotTo(HaveOccurred())

		return checkInvocation(cli.NewRoot(ctx, cli.App{}), args)
	}

	DescribeTable("accepts a command line that parses",
		func(ctx SpecContext, line string) {
			Expect(check(ctx, line)).To(Succeed())
		},
		Entry("a command with a global flag", "coroner version --json"),
		Entry("a positional and repeated flags", `coroner timeline create mongo --title "Primary election storm" --ref A --ref B`),
		Entry("an optional positional left out", "coroner timeline view"),
		Entry("a global flag before the command", "coroner --db x.db timeline list"),
		Entry("a command's own shorthand flag", "coroner status -t mongo"),
		Entry("--help without the required positional", "coroner timeline create --help"),
		Entry("help naming a help topic", "coroner help time"),
		Entry("help naming the reference topic", "coroner help reference"),
		Entry("help naming a command", "coroner help timeline create"),
		Entry("help, which ignores --json", "coroner help version --json"),
		Entry("root's own flag", "coroner --version"),
		Entry("cobra's completion command", "coroner completion zsh"),
	)

	DescribeTable("rejects a command line that does not parse, saying why",
		func(ctx SpecContext, line, reason string) {
			Expect(check(ctx, line)).To(MatchError(ContainSubstring(reason)))
		},
		Entry("an unknown flag", "coroner timeline create mongo --sorce", "unknown flag: --sorce"),
		Entry("an unknown command", "coroner nosuch", `unknown command "nosuch" for "coroner"`),
		Entry("an unknown verb under a noun", "coroner timeline crate mongo", `unknown command "crate" for "coroner timeline"`),
		Entry("an abbreviated command", "coroner time create", `unknown command "time" for "coroner"`),
		Entry("too many positionals", "coroner timeline view a b", "accepts at most 1 arg(s), received 2"),
		Entry("a positional to a command that takes none", "coroner version extra", `unknown command "extra" for "coroner version"`),
		Entry("a format its command does not print", "coroner version --format jsonl", "cannot print --format jsonl"),
		Entry("--json with another --format", "coroner timeline create x --json --format text", "--json conflicts"),
		Entry("help naming neither a help topic nor a command", "coroner help nosuch", `unknown help topic or command "nosuch"`),
		Entry("-t twice to a one-timeline command", "coroner event add x -t a -t b", "-t"),
		Entry("-t beside a positional timeline", "coroner timeline view mongo -t other", "-t"),
	)

	DescribeTable("rejects a command line its commands' declarations forbid, saying why",
		func(line, reason string) {
			root := &cobra.Command{Use: "coroner"}
			// cobra prints deprecation notices through OutOrStderr.
			root.SetOut(GinkgoWriter)

			exclusive := &cobra.Command{Use: "exclusive", Run: func(*cobra.Command, []string) {}}
			exclusive.Flags().Bool("a", false, "")
			exclusive.Flags().Bool("b", false, "")
			exclusive.MarkFlagsMutuallyExclusive("a", "b")
			exclusive.Flags().Bool("old", false, "")
			Expect(exclusive.Flags().MarkDeprecated("old", "use --a")).To(Succeed())

			required := &cobra.Command{Use: "required", Run: func(*cobra.Command, []string) {}}
			required.Flags().String("name", "", "")
			Expect(required.MarkFlagRequired("name")).To(Succeed())

			legacy := &cobra.Command{Use: "legacy", Deprecated: "use exclusive", Run: func(*cobra.Command, []string) {}}
			root.AddCommand(exclusive, required, legacy)

			Expect(checkInvocation(root, strings.Fields(line))).To(MatchError(ContainSubstring(reason)))
		},
		Entry("flags declared mutually exclusive", "coroner exclusive --a --b", "none of the others can be"),
		Entry("a required flag left out", "coroner required", `required flag(s) "name" not set`),
		Entry("a deprecated flag", "coroner exclusive --old", "flag --old is deprecated, use --a"),
		Entry("a deprecated command", "coroner legacy", `command "coroner legacy" is deprecated, use exclusive`),
	)
})

var _ = Describe("checkExamples", func() {
	It("names the command, the line and the parse error of each broken example", func(ctx SpecContext) {
		tree := cli.NewRoot(ctx, cli.App{})
		create, _, err := tree.Find([]string{"timeline", "create"})
		Expect(err).NotTo(HaveOccurred())
		create.Example = "  $ coroner timeline create mongo\n  $ coroner timeline create mongo --sorce\n  $ coroner timeline create 'mongo"

		Expect(checkExamples(tree, freshRoots(ctx)).Broken).To(ConsistOf(
			"coroner timeline create:2: coroner timeline create mongo --sorce: unknown flag: --sorce",
			"coroner timeline create:3: coroner timeline create 'mongo: unterminated ' quote",
		))
	})

	It("checks hidden commands, and reports a line it cannot check as skipped", func(ctx SpecContext) {
		tree := cli.NewRoot(ctx, cli.App{})
		tree.AddCommand(&cobra.Command{
			Use: "legacy", Hidden: true,
			Example: "  $ coroner nosuch\n  $ coroner timeline use $(coroner timeline list | head -1)",
		})

		found := checkExamples(tree, freshRoots(ctx))

		Expect(found.Broken).To(ConsistOf(HavePrefix("coroner legacy:1: coroner nosuch: ")))
		Expect(found.Skipped).To(ConsistOf(HavePrefix("coroner legacy:2: ")))
	})

	It("reports an Example with no coroner line as skipped", func(ctx SpecContext) {
		tree := cli.NewRoot(ctx, cli.App{})
		tree.AddCommand(&cobra.Command{Use: "legacy", Example: "  $ corner timeline list"})

		Expect(checkExamples(tree, freshRoots(ctx)).Skipped).To(ConsistOf("coroner legacy: no coroner line"))
	})

	It("counts each line it parsed under every command, broken or not, and no skipped line", func(ctx SpecContext) {
		tree := &cobra.Command{Use: "coroner"}
		tree.AddCommand(&cobra.Command{
			Use:     "legacy",
			Example: "  $ coroner nosuch\n  $ coroner timeline use $(coroner timeline list | head -1)",
		})

		Expect(checkExamples(tree, freshRoots(ctx)).Checked).To(Equal(1))
	})

	It("finds no broken example among coroner's commands", func(ctx SpecContext) {
		found := checkExamples(cli.NewRoot(ctx, cli.App{}), freshRoots(ctx))

		for _, line := range found.Skipped {
			AddReportEntry("skipped example", line)
		}
		Expect(found.Broken).To(BeEmpty())
		Expect(found.Checked).To(BeNumerically(">", 0))
	})
})

var _ = Describe("checkDocExamples", func() {
	It("names the page, the line and the parse error of each broken example", func(ctx SpecContext) {
		docs := fstest.MapFS{
			"index.md":          {Data: []byte("```console\n$ coroner nosuch\n```\n")},
			"how-to/logs.md":    {Data: []byte("# Logs\n\n```sh\ncoroner version\ncoroner event add x --sorce slack\n```\n")},
			"how-to/logs.jsonl": {Data: []byte("coroner nosuch\n")},
		}

		found, err := checkDocExamples(docs, freshRoots(ctx))

		Expect(err).NotTo(HaveOccurred())
		Expect(found.Broken).To(ConsistOf(
			"how-to/logs.md:5: coroner event add x --sorce slack: unknown flag: --sorce",
			HavePrefix("index.md:2: coroner nosuch: "),
		))
	})

	DescribeTable("finds no broken example on any page",
		func(ctx SpecContext, dir string) {
			found, err := checkDocExamples(os.DirFS(dir), freshRoots(ctx))

			Expect(err).NotTo(HaveOccurred())
			for _, line := range found.Skipped {
				AddReportEntry("skipped example", line)
			}
			Expect(found.Broken).To(BeEmpty())
			Expect(found.Checked).To(BeNumerically(">", 0))
		},
		Entry("under docs", "../../docs"),
		Entry("of the agent skills, references included", "../../skills"),
	)
})

// freshRoots returns a func that builds a new coroner root command on each call.
func freshRoots(ctx SpecContext) func() *cobra.Command {
	return func() *cobra.Command { return cli.NewRoot(ctx, cli.App{}) }
}

// exampleReport is what a check of example lines found. Each entry reads
// "<source>:<line>: <text>: <reason>".
type exampleReport struct {
	Broken  []string
	Skipped []string
	// Checked is the number of lines parsed.
	Checked int
}

// checkDocExamples checks the coroner lines of every Markdown page in docs, each labelled with
// its path, as checkLines does.
func checkDocExamples(docs fs.FS, newRoot func() *cobra.Command) (exampleReport, error) {
	var lines []exampleLine
	err := fs.WalkDir(docs, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) != ".md" {
			return err
		}

		page, err := fs.ReadFile(docs, name)
		if err != nil {
			return err
		}
		lines = append(lines, markdownExampleLines(name, string(page))...)

		return nil
	})

	return checkLines(lines, newRoot), err
}

// checkExamples checks each coroner line in the Example of tree and every command under it,
// hidden ones included, as checkLines does. tree is walked separately so a caller can edit an
// Example without editing newRoot's trees.
func checkExamples(tree *cobra.Command, newRoot func() *cobra.Command) exampleReport {
	lines := exampleLines(tree.CommandPath(), tree.Example)
	found := checkLines(lines, newRoot)
	if tree.Example != "" && len(lines) == 0 {
		found.Skipped = append(found.Skipped, tree.CommandPath()+": no coroner line")
	}

	for _, child := range tree.Commands() {
		under := checkExamples(child, newRoot)
		found.Broken = append(found.Broken, under.Broken...)
		found.Skipped = append(found.Skipped, under.Skipped...)
		found.Checked += under.Checked
	}

	return found
}

// checkLines parses each line against a fresh tree from newRoot, one per line because a tree
// keeps the flag values it parsed, reports a line with Skipped set as skipped, and counts what
// it parsed.
func checkLines(lines []exampleLine, newRoot func() *cobra.Command) exampleReport {
	var found exampleReport

	for _, line := range lines {
		entry := func(reason any) string {
			// cobra's unknown-command error ends in a newline.
			return strings.TrimSpace(fmt.Sprintf("%s:%d: %s: %v", line.Source, line.Number, line.Text, reason))
		}

		if line.Skipped != "" {
			found.Skipped = append(found.Skipped, entry(line.Skipped))
			continue
		}

		found.Checked++
		args, err := splitWords(line.Text)
		if err == nil {
			err = checkInvocation(newRoot(), args)
		}
		if err != nil {
			found.Broken = append(found.Broken, entry(err))
		}
	}

	return found
}

// checkInvocation reports why args, a coroner command line whose args[0] is the word coroner,
// would not reach its command's body, without running it: the checks cobra makes before
// running a command, a deprecated command or flag, checkCommandLine's --format, --json, -t,
// --overlay and positional timeline checks, and that help names a help topic or a command.
// Checks that need the environment or the database, such as which timeline is current, are
// not made.
func checkInvocation(root *cobra.Command, args []string) error {
	// Find matches whole names only (H-7): no prefix matching.
	cmd, rest, err := root.Find(args[1:])
	if err != nil {
		return err
	}

	err = cmd.ParseFlags(rest)
	if err != nil {
		return err
	}

	err = deprecations(cmd)
	if err != nil {
		return err
	}

	// cobra prints help for --help before it validates anything else.
	if help, _ := cmd.Flags().GetBool("help"); help {
		return nil
	}

	positionals := cmd.Flags().Args()
	// Find leaves an unknown verb under a noun as a positional, which runNoun rejects.
	if cmd.HasSubCommands() && len(positionals) > 0 {
		return fmt.Errorf("unknown command %q for %q", positionals[0], cmd.CommandPath())
	}

	return errors.Join(
		cmd.ValidateArgs(positionals), cmd.ValidateRequiredFlags(), cmd.ValidateFlagGroups(),
		coronerChecks(root, cmd, positionals),
	)
}

// deprecations reports cmd if it is deprecated, and each deprecated flag it parsed.
func deprecations(cmd *cobra.Command) error {
	var found []error
	if cmd.Deprecated != "" {
		found = append(found, fmt.Errorf("command %q is deprecated, %s", cmd.CommandPath(), cmd.Deprecated))
	}

	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Deprecated != "" {
			found = append(found, fmt.Errorf("flag --%s is deprecated, %s", flag.Name, flag.Deprecated))
		}
	})

	return errors.Join(found...)
}

// coronerChecks makes the checks coroner adds before a command's body: help's target, or
// checkCommandLine on any other command with a RunE.
func coronerChecks(root, cmd *cobra.Command, positionals []string) error {
	switch {
	// NewRoot adds help after wrapRunErrors, so help skips checkCommandLine.
	case cmd.Parent() == root && cmd.Name() == "help":
		return helpTarget(root, positionals)
	case cmd.RunE != nil:
		return cli.CheckCommandLine(cmd, positionals)
	}

	return nil
}

// helpTarget reports whether help's positionals, if any, start with a help topic or name a
// command.
func helpTarget(root *cobra.Command, positionals []string) error {
	if len(positionals) == 0 {
		return nil
	}

	isTopic := func(topic cli.HelpTopic) bool { return topic.Name == positionals[0] }
	_, _, err := root.Find(positionals)
	if err != nil && !slices.ContainsFunc(cli.CommandTopics(), isTopic) {
		return fmt.Errorf("unknown help topic or command %q", positionals[0])
	}

	return nil
}

// exampleLine is a line of a text that invokes coroner.
type exampleLine struct {
	// Source is what the text came from, such as a command path.
	Source string
	// Number is the line of the text the invocation starts on, from 1.
	Number int
	// Text runs from the word coroner to the end of the line, or is the whole line when coroner
	// is not a command word in it.
	Text string
	// Skipped is why the line is not checked, or empty.
	Skipped string
}

// exampleLines returns each line of text where coroner is a word, labelled with source. coroner
// is a command word at the start of the line, after a $ or PS> prompt, or after |, ;, & or (,
// once past any of do, then, else, if, while, until, time, sudo, exec, command, env or ! and
// any NAME=value assignments, and the line is returned from that word on. A line ending in a
// backslash continues on the next. Lines starting with # are ignored, and a line where coroner
// is not a command word, or with $( or a backtick, is returned with Skipped set.
func exampleLines(source, text string) []exampleLine {
	commandWord := regexp.MustCompile(`((?:^(?:(?:\$|PS>)\s+)?|[|;&(]\s*)(?:(?:do|then|else|if|while|until|time|sudo|exec|command|env|!)\s+)*(?:[A-Za-z_]\w*=\S*\s+)*)coroner(?:[\s)]|$)`)
	anyWord := regexp.MustCompile(`(?:^|[\s|;&(])coroner(?:[\s)]|$)`)
	var lines []exampleLine

	for number, line := range logicalLines(text) {
		if strings.HasPrefix(line, "#") || !anyWord.MatchString(line) {
			continue
		}

		found := exampleLine{Source: source, Number: number, Text: line}
		match := commandWord.FindStringSubmatchIndex(line)
		if match != nil {
			found.Text = line[match[3]:]
		}

		switch {
		case match == nil:
			found.Skipped = "coroner is not a command word"
		case strings.Contains(line, "$("):
			found.Skipped = "command substitution"
		case strings.Contains(line, "`"):
			found.Skipped = "backtick"
		}

		lines = append(lines, found)
	}

	return lines
}

// markdownExampleLines returns exampleLines of the shell code blocks in markdown, numbered by
// their line in markdown: every line of a sh, bash, shell or zsh block, and of a console block
// only the lines starting with a $ prompt and the lines they continue onto.
func markdownExampleLines(source, markdown string) []exampleLine {
	lines := strings.Split(markdown, "\n")
	fence, language, continued := "", "", false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		keep := false

		switch {
		case fence == "":
			fence, language = openingFence(trimmed)
		case len(trimmed) >= len(fence) && strings.Trim(trimmed, fence[:1]) == "":
			fence = ""
		case language == "console":
			keep = continued || strings.HasPrefix(trimmed, "$ ")
		default:
			keep = slices.Contains([]string{"sh", "bash", "shell", "zsh"}, language)
		}

		continued = keep && strings.HasSuffix(trimmed, `\`)
		if !keep {
			lines[i] = ""
		}
	}

	return exampleLines(source, strings.Join(lines, "\n"))
}

// openingFence returns the run of three or more backticks or tildes that line opens a code block
// with, and the first word of its info string as the block's language, or two empty strings.
func openingFence(line string) (string, string) {
	info := strings.TrimLeft(line, "`")
	if len(line)-len(info) < 3 {
		info = strings.TrimLeft(line, "~")
	}

	fence := line[:len(line)-len(info)]
	if len(fence) < 3 {
		return "", ""
	}

	language, _, _ := strings.Cut(strings.TrimSpace(info), " ")

	return fence, language
}

// logicalLines yields the trimmed lines of text, each keyed by its first line's number, with a
// line ending in a backslash joined to the next by a space, unless it starts with #.
func logicalLines(text string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		var parts []string
		start := 0

		for i, line := range strings.Split(text, "\n") {
			if parts == nil {
				start = i + 1
			}

			trimmed := strings.TrimSpace(line)
			body, continues := strings.CutSuffix(trimmed, `\`)
			parts = append(parts, strings.TrimSpace(body))
			if continues && !strings.HasPrefix(trimmed, "#") {
				continue
			}

			if !yield(start, strings.Join(parts, " ")) {
				return
			}
			parts = nil
		}

		if parts != nil {
			yield(start, strings.Join(parts, " "))
		}
	}
}

// splitWords splits line into words, honouring quotes and backslash escapes (a backslash always
// escapes, even inside double quotes), up to the first unquoted |, ;, &, <, > or ), or a word
// starting with #. It expands nothing.
func splitWords(line string) ([]string, error) {
	var (
		words   []string
		word    strings.Builder
		inWord  bool
		escaped bool
		quote   rune
	)

scan:
	for _, r := range line {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '<' || r == '>':
			// Digits before a redirection name a file descriptor, as in 2>&1.
			if word.Len() > 0 && strings.Trim(word.String(), "0123456789") == "" {
				inWord = false
			}
			break scan
		case strings.ContainsRune("|;&)", r), r == '#' && !inWord:
			break scan
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}

	switch {
	case escaped:
		return nil, errors.New("trailing backslash")
	case quote != 0:
		return nil, fmt.Errorf("unterminated %c quote", quote)
	case inWord:
		words = append(words, word.String())
	}

	return words, nil
}
