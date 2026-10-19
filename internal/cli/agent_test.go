package cli_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/skills"
)

const skillDir = "../../skills/coroner"

var _ = Describe("agent skill", func() {
	It("prints skills/coroner/SKILL.md byte for byte without opening the database", func(ctx SpecContext) {
		var out, errOut bytes.Buffer
		want, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		Expect(err).NotTo(HaveOccurred())

		// No OpenStore: calling it would panic, and Run would exit 1.
		code := cli.Run(ctx, cli.App{
			Args:      []string{"agent", "skill"},
			Out:       &out,
			Err:       &errOut,
			LookupEnv: func(string) (string, bool) { return "", false },
			Skill:     skills.Coroner,
		})

		Expect(code).To(Equal(0))
		Expect(errOut.String()).To(BeEmpty())
		Expect(out.String()).To(Equal(string(want)))
	})

	It("keeps SKILL.md's front matter to the spec keys coroner uses", func() {
		doc, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		Expect(err).NotTo(HaveOccurred())

		fields := frontMatter(string(doc))

		Expect(slices.Sorted(maps.Keys(fields))).To(Equal([]string{"compatibility", "description", "name"}))
		Expect(fields["name"]).To(Equal(filepath.Base(skillDir)))
		Expect(fields["description"]).NotTo(BeEmpty())
		Expect(len(fields["description"])).To(BeNumerically("<=", 1024))
		Expect(len(fields["compatibility"])).To(BeNumerically("<=", 500))
	})

	It("links only files that exist", func() {
		references, err := filepath.Glob(filepath.Join(skillDir, "references", "*.md"))
		Expect(err).NotTo(HaveOccurred())
		pages := append([]string{filepath.Join(skillDir, "SKILL.md")}, references...)

		for _, page := range pages {
			doc, err := os.ReadFile(filepath.Clean(page))
			Expect(err).NotTo(HaveOccurred())
			for _, target := range relativeLinks(string(doc)) {
				Expect(filepath.Join(filepath.Dir(page), target)).To(BeAnExistingFile(), "linked from %s", page)
			}
		}
	})
})

// frontMatter reads the plain key: value lines between a document's leading --- lines.
func frontMatter(doc string) map[string]string {
	block, _, _ := strings.Cut(strings.TrimPrefix(doc, "---\n"), "\n---\n")
	fields := map[string]string{}
	for line := range strings.Lines(block) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		fields[key] = strings.TrimSpace(value)
	}
	return fields
}

var markdownLink = regexp.MustCompile(`\]\(([^)#\s]+)(?:#[^)]*)?\)`)

// relativeLinks returns the target of each link in doc that names a file beside it, not a URL.
func relativeLinks(doc string) []string {
	var targets []string
	for _, match := range markdownLink.FindAllStringSubmatch(doc, -1) {
		if !strings.Contains(match[1], ":") {
			targets = append(targets, match[1])
		}
	}
	return targets
}
