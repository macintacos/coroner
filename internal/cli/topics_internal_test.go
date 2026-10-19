package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = DescribeTable("closest returns the candidate the fewest edits away, within two, the first on a tie",
	func(name string, candidates []string, want string) {
		Expect(closest(name, candidates)).To(Equal(want))
	},
	Entry("transposed letters", "tmie", []string{"environment", "time", "version"}, "time"),
	Entry("a missing letter", "envronment", []string{"time", "environment"}, "environment"),
	Entry("an extra letter", "versions", []string{"version", "help"}, "version"),
	Entry("a name close to nothing", "qqqq", []string{"environment", "help"}, ""),
	Entry("a tie", "ab", []string{"ax", "xb"}, "ax"),
)

var _ = Describe("help template", func() {
	It("aligns root help's commands and topics in one column, however long a topic's name", func() {
		root := &cobra.Command{Use: "coroner"}
		root.AddCommand(&cobra.Command{Use: "version", Short: "Show the version", Run: func(*cobra.Command, []string) {}})
		var out strings.Builder

		Expect(newHelpTemplate([]HelpTopic{{Name: "a-long-topic-name", Short: "About it"}}).Execute(&out, root)).To(Succeed())

		Expect(out.String()).To(ContainSubstring("\n  version            Show the version\n"))
		Expect(out.String()).To(ContainSubstring("\n  a-long-topic-name  About it\n"))
	})
})

var _ = Describe("help topics", func() {
	It("list every error kind in exit-codes under the exit code it exits with", func() {
		table := documentedExitCodes(topicBody("exit-codes"))

		codes := map[int]bool{0: true}
		for _, code := range exitCodes() {
			codes[code] = true
		}
		Expect(table.codes).To(Equal(codes))
		Expect(table.kinds).To(Equal(exitCodes()))
		Expect(table.duplicates).To(BeEmpty(), "kinds listed under two codes")
	})

	It("name in environment every environment variable the code reads", func() {
		keys := envKeysRead(filepath.Join("..", ".."))
		Expect(keys).To(ContainElements("CORONER_DB", "HOME"), "the scan finds known reads")

		body := topicBody("environment")
		for _, key := range keys {
			Expect(body).To(ContainSubstring("`"+key+"`"), key)
		}
	})
})

func topicBody(name string) string {
	GinkgoHelper()
	topic, ok := topicNamed(HelpTopics(), name)
	Expect(ok).To(BeTrue(), name)

	return topic.Body
}

type exitCodeTable struct {
	codes      map[int]bool
	kinds      map[Kind]int
	duplicates []Kind
}

// documentedExitCodes reads each "| code | meaning | kinds |" table row: its code, and the
// backticked kinds in its last column.
func documentedExitCodes(body string) exitCodeTable {
	// The outer pipes leave an empty cell at each end of a three-column row.
	const rowCells = 5
	kindPattern := regexp.MustCompile("`([a-z_]+)`")
	table := exitCodeTable{codes: map[int]bool{}, kinds: map[Kind]int{}}

	for line := range strings.Lines(body) {
		cells := strings.Split(strings.TrimSpace(line), "|")
		if len(cells) != rowCells {
			continue
		}

		code, err := strconv.Atoi(strings.TrimSpace(cells[1]))
		if err != nil {
			continue
		}

		table.codes[code] = true
		for _, match := range kindPattern.FindAllStringSubmatch(cells[3], -1) {
			kind := Kind(match[1])
			if _, ok := table.kinds[kind]; ok {
				table.duplicates = append(table.duplicates, kind)
			}
			table.kinds[kind] = code
		}
	}

	return table
}

// envKeysRead returns the environment variable names that non-test Go files under root pass
// to a call whose function, or another argument, is named like env: lookupEnv("TERM"),
// xdgHome(env, "XDG_DATA_HOME", …).
// ponytail: a name read through a constant escapes the scan; resolve identifiers to their
// constants if one ever is.
func envKeysRead(root string) []string {
	GinkgoHelper()
	keyPattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	envWord := regexp.MustCompile(`(^env|Env)([A-Z]|$)`)
	keys := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() && path != root && unscanned(root, path):
			return fs.SkipDir
		case entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go"):
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !namedLikeEnv(envWord, append([]ast.Expr{call.Fun}, call.Args...)) {
				return true
			}

			for _, arg := range call.Args {
				literal, ok := arg.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}

				// The parser only yields well-formed string literals.
				key, _ := strconv.Unquote(literal.Value)
				if keyPattern.MatchString(key) {
					keys[key] = true
				}
			}

			return true
		})

		return nil
	})
	Expect(err).NotTo(HaveOccurred())

	return slices.Sorted(maps.Keys(keys))
}

// unscanned reports whether envKeysRead skips the directory at path: test fixtures, which
// may not parse, hidden and scratch directories, and dev tools, whose reads are not
// user-facing.
func unscanned(root, path string) bool {
	name := filepath.Base(path)
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		path == filepath.Join(root, "internal", "tools")
}

func namedLikeEnv(envWord *regexp.Regexp, exprs []ast.Expr) bool {
	return slices.ContainsFunc(exprs, func(expr ast.Expr) bool {
		var name string
		switch expr := expr.(type) {
		case *ast.Ident:
			name = expr.Name
		case *ast.SelectorExpr:
			name = expr.Sel.Name
		}

		return envWord.MatchString(name)
	})
}
