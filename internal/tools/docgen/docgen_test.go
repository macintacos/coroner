package main

import (
	"bytes"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli"
)

// newReference builds a fresh reference per call: generating pages mutates its root, adding a help command.
func newReference(topics ...cli.HelpTopic) reference {
	root := &cobra.Command{Use: "tool", Short: "A tool"}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(&cobra.Command{Use: "sub", Short: "A subcommand", Run: func(*cobra.Command, []string) {}})

	return reference{root: root, topics: topics}
}

var _ = Describe("docgen", func() {
	var dir string

	BeforeEach(func() {
		dir = filepath.Join(GinkgoT().TempDir(), "cli")
		Expect(write(newReference(), dir)).To(Succeed())
	})

	It("writes one page per command, each opening on its title and carrying no generation date", func() {
		pages, err := readPages(os.DirFS(dir))
		Expect(err).NotTo(HaveOccurred())
		Expect(pages).To(SatisfyAll(HaveLen(2), HaveKey("tool.md"), HaveKey("tool_sub.md")))

		for name, content := range pages {
			Expect(string(content)).NotTo(ContainSubstring("Auto generated"), name)
		}

		Expect(string(pages["tool_sub.md"])).To(HavePrefix("---\ntitle: tool sub\n---\n\n"))
	})

	It("accepts a tree that matches the commands, printing nothing", func() {
		var out bytes.Buffer

		Expect(check(newReference(), dir, &out)).To(Succeed())
		Expect(out.String()).To(BeEmpty())
	})

	It("accepts a matching tree that also holds files other than pages", func() {
		Expect(os.WriteFile(filepath.Join(dir, ".DS_Store"), nil, 0o600)).To(Succeed())
		var out bytes.Buffer

		Expect(check(newReference(), dir, &out)).To(Succeed())
		Expect(out.String()).To(BeEmpty())
	})

	It("lists every page as added when the tree is missing", func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
		var out bytes.Buffer

		Expect(check(newReference(), dir, &out)).NotTo(Succeed())
		Expect(out.String()).To(ContainSubstring("added: " + filepath.Join(dir, "tool.md") + "\n"))
	})

	It("lists every added, changed and removed page and fails when the tree drifts", func() {
		Expect(os.Remove(filepath.Join(dir, "tool_sub.md"))).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "tool.md"), []byte("edited\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "tool_gone.md"), []byte("stale\n"), 0o600)).To(Succeed())
		var out bytes.Buffer

		Expect(check(newReference(), dir, &out)).To(MatchError(ContainSubstring("go run ./internal/tools/docgen")))
		Expect(out.String()).To(Equal(
			"changed: " + filepath.Join(dir, "tool.md") + "\n" +
				"removed: " + filepath.Join(dir, "tool_gone.md") + "\n" +
				"added: " + filepath.Join(dir, "tool_sub.md") + "\n"))
	})

	It("rewrites a drifted tree into one check accepts, dropping pages of removed commands", func() {
		Expect(os.WriteFile(filepath.Join(dir, "tool_gone.md"), []byte("stale\n"), 0o600)).To(Succeed())

		Expect(write(newReference(), dir)).To(Succeed())

		Expect(check(newReference(), dir, &bytes.Buffer{})).To(Succeed())
	})

	Describe("help topics", func() {
		topic := cli.HelpTopic{Name: "time", Short: "Reading times", Body: "# Reading times\n\nUse RFC 3339.\n"}

		It("writes a page per topic, titled as its help command, holding the topic's source", func() {
			Expect(write(newReference(topic), dir)).To(Succeed())

			pages, err := readPages(os.DirFS(dir))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(pages["tool_help_time.md"])).To(Equal("---\ntitle: tool help time\n---\n\n# Reading times\n\nUse RFC 3339.\n"))
		})

		It("lists a topic page whose source changed", func() {
			Expect(write(newReference(topic), dir)).To(Succeed())
			edited := topic
			edited.Body += "Or an epoch.\n"
			var out bytes.Buffer

			Expect(check(newReference(edited), dir, &out)).NotTo(Succeed())
			Expect(out.String()).To(Equal("changed: " + filepath.Join(dir, "tool_help_time.md") + "\n"))
		})
	})
})
