package cli_test

import (
	"errors"
	"fmt"
	"iter"
	"regexp"
	"strings"
	"unicode"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

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

	It("ignores comments, and lines where coroner is not a command word", func() {
		text := "  # coroner timeline list\n" +
			"  $ cp coroner.fish ~/.config/fish/completions/coroner.fish\n" +
			"  $ ls \"${fpath[1]}/_coroner\""

		Expect(exampleLines("s", text)).To(BeEmpty())
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
	)
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
		Entry("a help topic", "coroner help time"),
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
	)

	It("rejects flags its command declares mutually exclusive", func() {
		root := &cobra.Command{Use: "coroner"}
		sub := &cobra.Command{Use: "sub", Run: func(*cobra.Command, []string) {}}
		sub.Flags().Bool("a", false, "")
		sub.Flags().Bool("b", false, "")
		sub.MarkFlagsMutuallyExclusive("a", "b")
		root.AddCommand(sub)

		Expect(checkInvocation(root, []string{"coroner", "sub", "--a", "--b"})).To(MatchError(ContainSubstring("none of the others can be")))
	})
})

var _ = Describe("checkExamples", func() {
	newRoot := func(ctx SpecContext) func() *cobra.Command {
		return func() *cobra.Command { return cli.NewRoot(ctx, cli.App{}) }
	}

	It("names the command, the line and the parse error of each broken example", func(ctx SpecContext) {
		tree := cli.NewRoot(ctx, cli.App{})
		create, _, err := tree.Find([]string{"timeline", "create"})
		Expect(err).NotTo(HaveOccurred())
		create.Example = "  $ coroner timeline create mongo\n  $ coroner timeline create mongo --sorce\n  $ coroner timeline create 'mongo"

		Expect(checkExamples(tree, newRoot(ctx)).Broken).To(ConsistOf(
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

		found := checkExamples(tree, newRoot(ctx))

		Expect(found.Broken).To(ConsistOf(HavePrefix("coroner legacy:1: coroner nosuch: ")))
		Expect(found.Skipped).To(ConsistOf(HavePrefix("coroner legacy:2: ")))
	})

	It("finds no broken example among coroner's commands", func(ctx SpecContext) {
		found := checkExamples(cli.NewRoot(ctx, cli.App{}), newRoot(ctx))

		for _, line := range found.Skipped {
			AddReportEntry("skipped example", line)
		}
		Expect(found.Broken).To(BeEmpty())
	})
})

// exampleReport is what checkExamples found. Each entry reads
// "<command path>:<line>: <text>: <reason>".
type exampleReport struct {
	// Broken are the lines that do not parse.
	Broken []string
	// Skipped are the lines not checked.
	Skipped []string
}

// checkExamples checks each coroner line in the Example of tree and every command under it,
// hidden ones included, against its own tree from newRoot: flag values persist on a tree once
// parsed.
func checkExamples(tree *cobra.Command, newRoot func() *cobra.Command) exampleReport {
	var found exampleReport

	for _, line := range exampleLines(tree.CommandPath(), tree.Example) {
		entry := func(reason any) string {
			return strings.TrimSpace(fmt.Sprintf("%s:%d: %s: %v", line.Source, line.Number, line.Text, reason))
		}

		if line.Skipped != "" {
			found.Skipped = append(found.Skipped, entry(line.Skipped))
			continue
		}

		args, err := splitWords(line.Text)
		if err == nil {
			err = checkInvocation(newRoot(), args)
		}
		if err != nil {
			found.Broken = append(found.Broken, entry(err))
		}
	}

	for _, child := range tree.Commands() {
		under := checkExamples(child, newRoot)
		found.Broken = append(found.Broken, under.Broken...)
		found.Skipped = append(found.Skipped, under.Skipped...)
	}

	return found
}

// checkInvocation reports why args, a coroner command line, would not reach its command's
// body: the checks cobra makes before running a command, without running it.
func checkInvocation(root *cobra.Command, args []string) error {
	// Find matches whole names only, so an abbreviated command fails here (H-7).
	cmd, rest, err := root.Find(args[1:])
	if err != nil {
		return err
	}

	err = cmd.ParseFlags(rest)
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

	return errors.Join(cmd.ValidateArgs(positionals), cmd.ValidateRequiredFlags(), cmd.ValidateFlagGroups())
}

// exampleLine is a line of a text that invokes coroner.
type exampleLine struct {
	// Source names the text: a command path, or a file.
	Source string
	// Number is the line of the text the invocation starts on, from 1.
	Number int
	// Text runs from the word coroner to the end of the line.
	Text string
	// Skipped is why the line is not checked, or empty.
	Skipped string
}

// exampleLines returns the lines of text that invoke coroner, labelled with source. A line
// ending in a backslash continues on the next.
func exampleLines(source, text string) []exampleLine {
	commandWord := regexp.MustCompile(`(^|[\s|;&(])coroner([\s)]|$)`)
	var lines []exampleLine

	for number, line := range logicalLines(text) {
		match := commandWord.FindStringSubmatchIndex(line)
		if match == nil || strings.HasPrefix(line, "#") {
			continue
		}

		found := exampleLine{Source: source, Number: number, Text: line[match[3]:]}
		switch {
		case strings.Contains(line, "$("):
			found.Skipped = "command substitution"
		case strings.Contains(line, "`"):
			found.Skipped = "backtick"
		}

		lines = append(lines, found)
	}

	return lines
}

// logicalLines yields the trimmed lines of text, each keyed by its first line's number, with a
// line ending in a backslash joined to the next by a space.
func logicalLines(text string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		var parts []string
		start := 0

		for i, line := range strings.Split(text, "\n") {
			if parts == nil {
				start = i + 1
			}

			body, continues := strings.CutSuffix(strings.TrimSpace(line), `\`)
			parts = append(parts, strings.TrimSpace(body))
			if continues {
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

// splitWords splits line into words as a POSIX shell does for quotes and backslashes, up to
// the first unquoted |, ;, &, < or >. It expands nothing.
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
		case strings.ContainsRune("|;&<>", r):
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
