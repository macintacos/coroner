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
		context := readRepoFile("CONTEXT.md")
		_, glossary, found := strings.Cut(context, "\n## Language\n")
		Expect(found).To(BeTrue(), "CONTEXT.md has a Language section")

		page := readRepoFile("docs", "explanation", "data-model.md")

		Expect(words(page)).To(ContainSubstring(words(glossary)))
	})
})

func readRepoFile(path ...string) string {
	GinkgoHelper()
	content, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
	Expect(err).NotTo(HaveOccurred())

	return string(content)
}

// words joins text's lines, dropping the backslash that ends a Markdown hard line break,
// which the docs site renders literally.
func words(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "\\\n", "\n")), " ")
}
