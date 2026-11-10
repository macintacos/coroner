package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"

	"charm.land/huh/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

var _ = Describe("the attach field's completions", func() {
	var root string
	var dirsRead []string
	rooted := func(name string) ([]fs.DirEntry, error) { return os.ReadDir(filepath.Join(root, name)) }
	spy := func(name string) ([]fs.DirEntry, error) {
		dirsRead = append(dirsRead, name)
		return rooted(name)
	}
	failing := func(string) ([]fs.DirEntry, error) { return nil, fs.ErrPermission }

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		dirsRead = nil
		Expect(os.CopyFS(root, fstest.MapFS{"files/p99.png": {}, "files/q01.png": {}, "files/.keep": {}, "docs/r1.txt": {}})).To(Succeed())
	})

	It("lists the entries of the typed path's directory that start with the typed name", func() {
		Expect(clikit.AttachCompletions(rooted, "")("files/p")).To(Equal([]string{"files/p99.png"}))
		Expect(clikit.AttachCompletions(rooted, "")("files/")).To(Equal([]string{"files/p99.png", "files/q01.png"}))
	})

	It("ends a directory with /", func() {
		Expect(clikit.AttachCompletions(rooted, "")("fi")).To(Equal([]string{"files/"}))
	})

	It("ends a symlink to a directory with /, and not one to a file", func() {
		Expect(os.Symlink("files", filepath.Join(root, "linked"))).To(Succeed())
		Expect(os.Symlink("docs/r1.txt", filepath.Join(root, "lnfile"))).To(Succeed())
		Expect(clikit.AttachCompletions(rooted, "")("l")).To(Equal([]string{"linked/", "lnfile"}))
	})

	It("matches the typed name case-sensitively", func() {
		fsys := fstest.MapFS{"Files/p99.png": {}}
		readDir := func(name string) ([]fs.DirEntry, error) { return fs.ReadDir(fsys, name) }
		Expect(clikit.AttachCompletions(readDir, "")("fi")).To(BeEmpty())
		Expect(clikit.AttachCompletions(readDir, "")("Fi")).To(Equal([]string{"Files/"}))
	})

	It("completes an absolute path the same way", func() {
		Expect(clikit.AttachCompletions(os.ReadDir, "")(root + "/files/p")).To(Equal([]string{root + "/files/p99.png"}))
	})

	It("leaves out a name starting with . until one is typed", func() {
		Expect(clikit.AttachCompletions(rooted, "")("files/")).NotTo(ContainElement("files/.keep"))
		Expect(clikit.AttachCompletions(rooted, "")("files/.")).To(Equal([]string{"files/.keep"}))
	})

	It("suggests nothing until something is typed, and reads no directory", func() {
		Expect(clikit.AttachCompletions(spy, "")("")).To(BeEmpty())
		Expect(dirsRead).To(BeEmpty())
	})

	It("completes a path starting with ~/ in the home directory, keeping ~/", func() {
		Expect(clikit.AttachCompletions(os.ReadDir, root)("~/")).To(Equal([]string{"~/docs/", "~/files/"}))
		Expect(clikit.AttachCompletions(os.ReadDir, root)("~/files/p")).To(Equal([]string{"~/files/p99.png"}))
	})

	It("suggests nothing for a path starting with ~/ without a home directory, and reads no directory", func() {
		Expect(clikit.AttachCompletions(spy, "")("~/fi")).To(BeEmpty())
		Expect(dirsRead).To(BeEmpty())
	})

	It("attaches a typed path starting with ~/ from the home directory", func() {
		d := clikit.Definition{Fields: []clikit.Field{clikit.AttachField()}}
		fields := d.FormFields(formCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{Paths: map[string]func(string) string{"attach": clikit.ExpandHome("/home/priya")}})
		attach := namedField(fields, "attach")
		attach.WithKeyMap(huh.NewDefaultKeyMap())
		attach.Focus()
		typeText(attach, "~/p99.png")
		attach.Blur()

		Expect(d.FormValues(fields)).To(Equal(clikit.FieldValues{"attach": {"/home/priya/p99.png"}}))
	})

	It("expands only a leading ~/, and nothing without a home directory", func() {
		Expect(clikit.ExpandHome("/home/priya")("~priya/p99.png")).To(Equal("~priya/p99.png"))
		Expect(clikit.ExpandHome("/home/priya")("docs/~/r1.txt")).To(Equal("docs/~/r1.txt"))
		Expect(clikit.ExpandHome("")("~/p99.png")).To(Equal("~/p99.png"))
	})

	It("suggests nothing, and no error, for a directory it cannot read", func() {
		Expect(clikit.AttachCompletions(failing, "")("files/p")).To(BeEmpty())

		choices := clikit.FormChoices{Completions: map[string]func(string) []string{"attach": clikit.AttachCompletions(failing, "")}}
		attach := namedField(clikit.Definition{Fields: []clikit.Field{clikit.AttachField()}}.FormFields(formCheckEnv(), clikit.FieldValues{}, choices), "attach")
		attach.WithKeyMap(huh.NewDefaultKeyMap())
		attach.Focus()
		typeText(attach, "files/p")

		Expect(attach.Error()).NotTo(HaveOccurred())
		Expect(fieldView(attach)).NotTo(ContainSubstring(clikit.SuggestionsLabel))
	})
})
