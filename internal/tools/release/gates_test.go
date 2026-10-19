package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	branchCmd    = "git rev-parse --abbrev-ref HEAD"
	statusCmd    = "git status --porcelain"
	fetchCmd     = "git fetch origin main"
	originCmd    = "git rev-parse --verify origin/main^{commit}"
	authCmd      = "gh auth status"
	repoNamesCmd = "gh secret list -R macintacos/coroner --json name"
	ciCmd        = "gh run list -R macintacos/coroner --workflow ci.yml --commit " + headSHA + " --json status,conclusion --limit 1"
)

// gatesFake answers every gate as passing.
func gatesFake() *fakeSystem {
	f := newFake()
	f.answers[branchCmd] = "main"
	f.answers[statusCmd] = ""
	f.answers[fetchCmd] = ""
	f.answers["git rev-parse --verify HEAD^{commit}"] = headSHA
	f.answers[originCmd] = headSHA
	f.answers[authCmd] = ""
	f.answers[repoNamesCmd] = `[{"name":"OTHER"},{"name":"HOMEBREW_TAP_GITHUB_TOKEN"}]`
	f.answers[ciCmd] = `[{"status":"completed","conclusion":"success"}]`

	return f
}

func miseCalls(f *fakeSystem) []string {
	var calls []string
	for _, c := range f.calls {
		if len(c) > 4 && c[:5] == "mise " {
			calls = append(calls, c)
		}
	}

	return calls
}

var _ = Describe("gates", func() {
	It("prints the released commit's full SHA when every gate passes", func() {
		f := gatesFake()
		got, code := runJSON(f, "gates")

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{"ok": true, "sha": headSHA}))
		Expect(miseCalls(f)).To(Equal([]string{"mise run preflight", "mise run release:check", "mise run release:snapshot"}))
	})

	DescribeTable("stops at the first failing gate with its own code",
		func(break_ func(*fakeSystem), want string) {
			f := gatesFake()
			break_(f)

			got, code := runJSON(f, "gates")

			Expect(code).To(Equal(1))
			Expect(got).To(HaveKeyWithValue("ok", false))
			Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", want)))
		},
		Entry("off main", func(f *fakeSystem) { f.answers[branchCmd] = "topic" }, "not_on_main"),
		Entry("a dirty tree", func(f *fakeSystem) { f.answers[statusCmd] = " M go.mod" }, "dirty_tree"),
		Entry("a failed fetch", func(f *fakeSystem) { delete(f.answers, fetchCmd) }, "git_error"),
		Entry("behind origin", func(f *fakeSystem) { f.answers[originCmd] = prevSHA }, "not_level_with_origin"),
		Entry("gh logged out", func(f *fakeSystem) { delete(f.answers, authCmd) }, "gh_unauthenticated"),
		Entry("no tap secret", func(f *fakeSystem) { f.answers[repoNamesCmd] = `[{"name":"OTHER"}]` }, "tap_secret_missing"),
		Entry("no CI run", func(f *fakeSystem) { f.answers[ciCmd] = `[]` }, "ci_not_green"),
		Entry("CI running", func(f *fakeSystem) { f.answers[ciCmd] = `[{"status":"in_progress","conclusion":""}]` }, "ci_not_green"),
		Entry("CI failed", func(f *fakeSystem) { f.answers[ciCmd] = `[{"status":"completed","conclusion":"failure"}]` }, "ci_not_green"),
		Entry("preflight failing", func(f *fakeSystem) { f.failing["mise run preflight"] = errFailed }, "preflight_failed"),
		Entry("release:check failing", func(f *fakeSystem) { f.failing["mise run release:check"] = errFailed }, "release_check_failed"),
		Entry("release:snapshot failing", func(f *fakeSystem) { f.failing["mise run release:snapshot"] = errFailed }, "release_snapshot_failed"),
	)

	It("runs no mise task once an earlier gate fails", func() {
		f := gatesFake()
		f.answers[ciCmd] = `[]`

		runJSON(f, "gates")

		Expect(miseCalls(f)).To(BeEmpty())
	})
})
