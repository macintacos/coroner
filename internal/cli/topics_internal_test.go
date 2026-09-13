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
)

var _ = DescribeTable("closest returns the candidate the fewest edits away, the first on a tie",
	func(name string, candidates []string, want string) {
		Expect(closest(name, candidates)).To(Equal(want))
	},
	Entry("transposed letters", "tmie", []string{"environment", "time", "version"}, "time"),
	Entry("a missing letter", "envronment", []string{"time", "environment"}, "environment"),
	Entry("an extra letter", "versions", []string{"version", "help"}, "version"),
	Entry("a name close to nothing", "qqqq", []string{"environment", "help"}, "help"),
	Entry("a tie", "ab", []string{"ax", "xb"}, "ax"),
)

var _ = Describe("help topics", func() {
	It("list every error kind in exit-codes under the exit code it exits with", func() {
		Expect(documentedExitCodes(topicBody("exit-codes"))).To(Equal(exitCodes()))
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
	topics := HelpTopics()
	i := slices.IndexFunc(topics, func(topic HelpTopic) bool { return topic.Name == name })
	ExpectWithOffset(1, i).NotTo(BeNumerically("<", 0), name)

	return topics[i].Body
}

// documentedExitCodes reads the kinds in the backticked last column of each "| code | … |"
// table row.
func documentedExitCodes(body string) map[Kind]int {
	kindPattern := regexp.MustCompile("`([a-z_]+)`")
	codes := map[Kind]int{}

	for line := range strings.Lines(body) {
		cells := strings.Split(strings.TrimSpace(line), "|")
		if len(cells) != 5 {
			continue
		}

		code, err := strconv.Atoi(strings.TrimSpace(cells[1]))
		if err != nil {
			continue
		}

		for _, match := range kindPattern.FindAllStringSubmatch(cells[3], -1) {
			kind := Kind(match[1])
			ExpectWithOffset(1, codes).NotTo(HaveKey(kind), "a kind listed under two codes")
			codes[kind] = code
		}
	}

	return codes
}

// envKeysRead returns the environment variable names that non-test Go files under root pass
// to a call whose function, or another argument, is named like env: lookupEnv("TERM"),
// xdgHome(env, "XDG_DATA_HOME", …).
// ponytail: a name read through a constant escapes the scan; resolve identifiers to their
// constants if one ever is.
func envKeysRead(root string) []string {
	keyPattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	keys := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return err
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !namedLikeEnv(append([]ast.Expr{call.Fun}, call.Args...)) {
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
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	return slices.Sorted(maps.Keys(keys))
}

func namedLikeEnv(exprs []ast.Expr) bool {
	return slices.ContainsFunc(exprs, func(expr ast.Expr) bool {
		var name string
		switch expr := expr.(type) {
		case *ast.Ident:
			name = expr.Name
		case *ast.SelectorExpr:
			name = expr.Sel.Name
		}

		return strings.Contains(strings.ToLower(name), "env")
	})
}
