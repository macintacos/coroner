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
