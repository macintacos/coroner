package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"

	"charm.land/huh/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the attach field's completions", func() {
	var dir string
	var read []string
	rooted := func(name string) ([]fs.DirEntry, error) { return os.ReadDir(filepath.Join(dir, name)) }
	spy := func(name string) ([]fs.DirEntry, error) {
		read = append(read, name)
		return rooted(name)
	}
	failing := func(string) ([]fs.DirEntry, error) { return nil, fs.ErrPermission }

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		read = nil
		Expect(os.CopyFS(dir, fstest.MapFS{"files/p99.png": {}, "files/q01.png": {}, "files/.keep": {}, "docs/r1.txt": {}})).To(Succeed())
	})

	It("lists the entries of the typed path's directory that start with the typed name", func() {
		Expect(attachCompletions(rooted)("files/p")).To(Equal([]string{"files/p99.png"}))
		Expect(attachCompletions(rooted)("files/")).To(Equal([]string{"files/p99.png", "files/q01.png"}))
	})

	It("ends a directory with /", func() {
		Expect(attachCompletions(rooted)("fi")).To(Equal([]string{"files/"}))
	})

	It("ends a symlink to a directory with /, and not one to a file", func() {
		Expect(os.Symlink("files", filepath.Join(dir, "linked"))).To(Succeed())
		Expect(os.Symlink("docs/r1.txt", filepath.Join(dir, "lnfile"))).To(Succeed())
		Expect(attachCompletions(rooted)("l")).To(Equal([]string{"linked/", "lnfile"}))
	})

	It("matches the typed name case-sensitively", func() {
		fsys := fstest.MapFS{"Files/p99.png": {}}
		readDir := func(name string) ([]fs.DirEntry, error) { return fs.ReadDir(fsys, name) }
		Expect(attachCompletions(readDir)("fi")).To(BeEmpty())
		Expect(attachCompletions(readDir)("Fi")).To(Equal([]string{"Files/"}))
	})

	It("completes an absolute path the same way", func() {
		Expect(attachCompletions(os.ReadDir)(dir + "/files/p")).To(Equal([]string{dir + "/files/p99.png"}))
	})

	It("leaves out a name starting with . until one is typed", func() {
		Expect(attachCompletions(rooted)("files/")).NotTo(ContainElement("files/.keep"))
		Expect(attachCompletions(rooted)("files/.")).To(Equal([]string{"files/.keep"}))
	})

	It("suggests nothing until something is typed, and reads no directory", func() {
		Expect(attachCompletions(spy)("")).To(BeEmpty())
		Expect(read).To(BeEmpty())
	})

	It("suggests nothing for a path starting with ~/, and reads no directory", func() {
		Expect(attachCompletions(spy)("~/fi")).To(BeEmpty())
		Expect(read).To(BeEmpty())
	})

	It("suggests nothing, and no error, for a directory it cannot read", func() {
		Expect(attachCompletions(failing)("files/p")).To(BeEmpty())

		choices := formChoices{completions: map[string]func(string) []string{"attach": attachCompletions(failing)}}
		f := namedField(definition{fields: []field{attachField()}}.formFields(formCheckEnv(), fieldValues{}, choices), "attach")
		f.WithKeyMap(huh.NewDefaultKeyMap())
		f.Focus()
		typeText(f, "files/p")

		Expect(f.Error()).NotTo(HaveOccurred())
		Expect(fieldView(f)).NotTo(ContainSubstring("↳"))
	})
})
