package timeline_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the data model page", func() {
	It("carries CONTEXT.md's glossary and prior art word for word", func() {
		// The page splits each _Avoid_ line into its own paragraph, since the docs site
		// renders a hard-break backslash literally; comparing words ignores that layout.
		glossary := strings.ReplaceAll(after(readRepoFile("CONTEXT.md"), "\n## Language\n"), "\\\n", "\n")
		page := after(after(readRepoFile("docs", "explanation", "data-model.md"), "\n## Glossary\n"), "\n### ")
		pageWords := words("### " + page)

		for entry := range strings.SplitSeq(strings.TrimSpace(glossary), "\n\n") {
			firstLine, _, _ := strings.Cut(entry, "\n")
			Expect(pageWords).To(ContainSubstring(words(entry)), firstLine)
		}
		Expect(pageWords).To(Equal(words(glossary)))
	})
})

func readRepoFile(path ...string) string {
	GinkgoHelper()
	content, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
	Expect(err).NotTo(HaveOccurred())

	return string(content)
}

func after(text, separator string) string {
	GinkgoHelper()
	_, rest, found := strings.Cut(text, separator)
	Expect(found).To(BeTrue(), "missing %q", separator)

	return rest
}

func words(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
