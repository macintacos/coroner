package main

import (
	"bytes"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("docgen", func() {
	var dir string

	BeforeEach(func() {
		dir = filepath.Join(GinkgoT().TempDir(), "cli")
		Expect(write(dir)).To(Succeed())
	})

	It("writes one page per command, each opening on its title and carrying no generation date", func() {
		pages, err := readTree(os.DirFS(dir))
		Expect(err).NotTo(HaveOccurred())
		Expect(pages).To(SatisfyAll(HaveKey("coroner.md"), HaveKey("coroner_version.md")))

		for name, content := range pages {
			Expect(string(content)).NotTo(ContainSubstring("Auto generated"), name)
		}

		Expect(string(pages["coroner_completion_bash.md"])).
			To(HavePrefix("---\ntitle: coroner completion bash\n---\n\n"))
	})

	It("accepts a tree that matches the commands, printing nothing", func() {
		var out bytes.Buffer

		Expect(check(dir, &out)).To(Succeed())
		Expect(out.String()).To(BeEmpty())
	})

	It("lists every added, changed and removed page and fails when the tree drifts", func() {
		Expect(os.Remove(filepath.Join(dir, "coroner_version.md"))).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "coroner.md"), []byte("edited\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "coroner_gone.md"), []byte("stale\n"), 0o600)).To(Succeed())
		var out bytes.Buffer

		Expect(check(dir, &out)).To(MatchError(ContainSubstring("go run ./internal/tools/docgen")))
		Expect(out.String()).To(Equal(
			"changed: " + filepath.Join(dir, "coroner.md") + "\n" +
				"removed: " + filepath.Join(dir, "coroner_gone.md") + "\n" +
				"added: " + filepath.Join(dir, "coroner_version.md") + "\n"))
	})

	It("rewrites a drifted tree into one check accepts, dropping pages of removed commands", func() {
		Expect(os.WriteFile(filepath.Join(dir, "coroner_gone.md"), []byte("stale\n"), 0o600)).To(Succeed())

		Expect(write(dir)).To(Succeed())

		Expect(check(dir, &bytes.Buffer{})).To(Succeed())
	})
})
