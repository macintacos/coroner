package main

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	rcTag        = "v0.1.0-rc.0"
	localTagCmd  = "git tag -l " + rcTag
	remoteTagCmd = "git ls-remote --tags origin refs/tags/" + rcTag
	pushCmd      = "git push origin refs/tags/" + rcTag
)

// tagFake answers a tag run where the tag exists nowhere yet and every git call succeeds.
func tagFake(notes string) (*fakeSystem, string) {
	path := filepath.Join(GinkgoT().TempDir(), "notes.md")
	Expect(os.WriteFile(path, []byte(notes), 0o600)).To(Succeed())

	f := newFake()
	f.answers["git rev-parse --verify HEAD^{commit}"] = headSHA
	f.answers[localTagCmd] = ""
	f.answers[remoteTagCmd] = ""
	f.answers["git tag -a --no-sign --cleanup=verbatim -F "+path+" "+rcTag+" "+headSHA] = ""
	f.answers[pushCmd] = ""
	f.answers["git tag -d "+rcTag] = ""

	return f, path
}

func tagWrites(f *fakeSystem) []string {
	var calls []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "git tag -a") || strings.HasPrefix(c, "git tag -d") || strings.HasPrefix(c, "git push") {
			calls = append(calls, c)
		}
	}

	return calls
}

var _ = Describe("tag", func() {
	It("creates the annotated tag from the notes, then pushes it", func() {
		f, notes := tagFake("### Added\n\n- a thing\n")

		got, code := runJSON(f, "tag", "--tag", rcTag, "--sha", "HEAD", "--notes-file", notes)

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{"ok": true, "tag": rcTag, "sha": headSHA, "dry_run": false, "pushed": true}))
		Expect(tagWrites(f)).To(Equal([]string{
			"git tag -a --no-sign --cleanup=verbatim -F " + notes + " " + rcTag + " " + headSHA,
			pushCmd,
		}))
	})

	It("checks everything but writes nothing on a dry run", func() {
		f, notes := tagFake("notes\n")

		got, code := runJSON(f, "tag", "--tag", rcTag, "--sha", "HEAD", "--notes-file", notes, "--dry-run")

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{"ok": true, "tag": rcTag, "sha": headSHA, "dry_run": true, "pushed": false}))
		Expect(f.calls).To(ContainElements(localTagCmd, remoteTagCmd))
		Expect(tagWrites(f)).To(BeEmpty())
	})

	It("deletes only the local tag when the push fails", func() {
		f, notes := tagFake("notes\n")
		delete(f.answers, pushCmd)

		got, code := runJSON(f, "tag", "--tag", rcTag, "--sha", "HEAD", "--notes-file", notes)

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "push_failed")))
		Expect(tagWrites(f)).To(Equal([]string{
			"git tag -a --no-sign --cleanup=verbatim -F " + notes + " " + rcTag + " " + headSHA,
			pushCmd,
			"git tag -d " + rcTag,
		}))
		Expect(f.calls).NotTo(ContainElement(ContainSubstring(":refs/tags/")))
		Expect(f.calls).NotTo(ContainElement(ContainSubstring("--delete")))
	})

	It("names a failed local delete in push_failed", func() {
		f, notes := tagFake("notes\n")
		delete(f.answers, pushCmd)
		delete(f.answers, "git tag -d "+rcTag)

		got, _ := runJSON(f, "tag", "--tag", rcTag, "--sha", "HEAD", "--notes-file", notes)

		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("message", ContainSubstring("git tag -d "+rcTag))))
	})

	DescribeTable("refuses to tag",
		func(tag, notes string, prepare func(*fakeSystem), want string) {
			f, path := tagFake(notes)
			prepare(f)

			got, code := runJSON(f, "tag", "--tag", tag, "--sha", "HEAD", "--notes-file", path)

			Expect(code).To(Equal(1))
			Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", want)))
			Expect(tagWrites(f)).To(BeEmpty())
		},
		Entry("a tag that is not a version", "release-1", "notes\n", func(*fakeSystem) {}, "bad_tag"),
		Entry("whitespace-only notes", rcTag, " \n\t\n", func(*fakeSystem) {}, "empty_notes"),
		Entry("a tag that exists locally", rcTag, "notes\n", func(f *fakeSystem) { f.answers[localTagCmd] = rcTag }, "tag_exists"),
		Entry("a tag that exists on origin", rcTag, "notes\n", func(f *fakeSystem) {
			f.answers[remoteTagCmd] = prevSHA + "\trefs/tags/" + rcTag
		}, "tag_exists"),
	)
})

var _ = Describe("tag-notes", func() {
	const (
		fetchTagCmd = "git fetch --force origin refs/tags/" + rcTag + ":refs/tags/" + rcTag
		typeCmd     = "git cat-file -t refs/tags/" + rcTag
		messageCmd  = "git for-each-ref --format=%(contents) refs/tags/" + rcTag
	)

	notesFake := func() *fakeSystem {
		f := newFake()
		f.answers[fetchTagCmd] = ""
		f.answers[typeCmd] = "tag"
		f.answers[messageCmd] = "### Added\n\n- a thing"

		return f
	}

	It("writes the tag's message byte for byte to the out file", func() {
		out := filepath.Join(GinkgoT().TempDir(), "release-notes.md")

		got, code := runJSON(notesFake(), "tag-notes", "--tag", rcTag, "--out", out)

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{"ok": true, "tag": rcTag, "notes_file": out}))
		Expect(os.ReadFile(filepath.Clean(out))).To(Equal([]byte("### Added\n\n- a thing")))
	})

	DescribeTable("fails without notes to publish",
		func(prepare func(*fakeSystem), want string) {
			f := notesFake()
			prepare(f)
			out := filepath.Join(GinkgoT().TempDir(), "release-notes.md")

			got, code := runJSON(f, "tag-notes", "--tag", rcTag, "--out", out)

			Expect(code).To(Equal(1))
			Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", want)))
			Expect(out).NotTo(BeAnExistingFile())
		},
		Entry("a tag origin lacks", func(f *fakeSystem) { delete(f.answers, fetchTagCmd) }, "tag_not_found"),
		Entry("a lightweight tag", func(f *fakeSystem) { f.answers[typeCmd] = "commit" }, "lightweight_tag"),
		Entry("a blank message", func(f *fakeSystem) { f.answers[messageCmd] = "" }, "empty_notes"),
	)
})
