package main

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	headSHA   = "1111111111111111111111111111111111111111"
	prevSHA   = "2222222222222222222222222222222222222222"
	emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	contractA = "internal/cli/testdata/script/contract/help.txtar"
)

const deprecationDiff = `diff --git a/internal/cli/event.go b/internal/cli/event.go
--- a/internal/cli/event.go
+++ b/internal/cli/event.go
@@ -10 +10 @@ func newEventCommand
-	deprecateFlag(cmd.Flags(), "gone",
+	deprecateFlag(cmd.Flags(), "old",
diff --git a/internal/cli/deprecation.go b/internal/cli/deprecation.go
--- a/internal/cli/deprecation.go
+++ b/internal/cli/deprecation.go
@@ -1,0 +2 @@
+func deprecateFlag(flags *pflag.FlagSet, name string) {
`

// computeFake answers a compute run over one PR and one stray commit, with the given tags
// on origin and diff range base.
func computeFake(lsRemote, base string) *fakeSystem {
	f := newFake()
	f.answers["git rev-parse --verify HEAD^{commit}"] = headSHA
	f.answers["git ls-remote --tags origin"] = lsRemote
	f.answers["git hash-object -t tree /dev/null"] = emptyTree
	logRange := headSHA
	if base != emptyTree {
		logRange = base + ".." + headSHA
	}
	f.answers["git log --format=%H%x1f%s "+logRange] = strings.Join([]string{
		"aaa\x1fEXC-1 Add a thing (#70)",
		"bbb\x1fdocs: tidy",
		"ccc\x1fEXC-1 follow-up (#70)",
	}, "\n")
	f.answers["gh pr view 70 -R macintacos/coroner --json number,title,body,url"] = `{"number":70,"title":"Add a thing","body":"- [ ] No contract change, or ` + "`stability:` `additive`" + `","url":"https://github.com/macintacos/coroner/pull/70"}`
	f.answers["git diff --name-status --no-renames "+base+" "+headSHA+" -- internal/cli/testdata/script/contract schema"] = "A\t" + contractA + "\nM\tschema/export-v1.schema.json\nD\tinternal/cli/testdata/script/contract/old.txtar"
	f.answers["git diff -U0 --no-renames "+base+" "+headSHA+" -- internal/cli :(exclude)internal/cli/*_test.go"] = deprecationDiff

	return f
}

var _ = Describe("compute", func() {
	It("prints the whole release plan as exactly one object", func() {
		got, code := runJSON(computeFake("", emptyTree), "compute", "minor", "--rc", "--sha", "HEAD")

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{
			"ok":           true,
			"bump":         "minor",
			"rc":           true,
			"sha":          headSHA,
			"previous_tag": nil,
			"tag":          "v0.1.0-rc.0",
			"compare_url":  nil,
			"pull_requests": []any{map[string]any{
				"number":    70.0,
				"title":     "Add a thing",
				"url":       "https://github.com/macintacos/coroner/pull/70",
				"body":      "- [ ] No contract change, or `stability:` `additive`",
				"stability": "additive",
				"commits": []any{
					map[string]any{"sha": "aaa", "subject": "EXC-1 Add a thing (#70)"},
					map[string]any{"sha": "ccc", "subject": "EXC-1 follow-up (#70)"},
				},
			}},
			"commits_without_pr": []any{map[string]any{"sha": "bbb", "subject": "docs: tidy"}},
			"contract_hints": map[string]any{
				"files": []any{
					map[string]any{"status": "added", "path": contractA},
					map[string]any{"status": "modified", "path": "schema/export-v1.schema.json"},
					map[string]any{"status": "deleted", "path": "internal/cli/testdata/script/contract/old.txtar"},
				},
				"deprecations": []any{
					map[string]any{"change": "removed", "path": "internal/cli/event.go", "line": "deprecateFlag(cmd.Flags(), \"gone\","},
					map[string]any{"change": "added", "path": "internal/cli/event.go", "line": "deprecateFlag(cmd.Flags(), \"old\","},
				},
			},
		}))
	})

	It("diffs from the latest final tag's peeled commit and links the comparison", func() {
		lsRemote := "9999\trefs/tags/v0.1.0\n" + prevSHA + "\trefs/tags/v0.1.0^{}\n3333\trefs/tags/v0.2.0-rc.0"
		got, code := runJSON(computeFake(lsRemote, prevSHA), "compute", "minor", "--sha", "HEAD")

		Expect(code).To(Equal(0))
		Expect(got).To(HaveKeyWithValue("previous_tag", "v0.1.0"))
		Expect(got).To(HaveKeyWithValue("tag", "v0.2.0"))
		Expect(got).To(HaveKeyWithValue("compare_url", "https://github.com/macintacos/coroner/compare/v0.1.0...v0.2.0"))
	})

	It("fails the whole call when a pull request cannot be read", func() {
		f := computeFake("", emptyTree)
		delete(f.answers, "gh pr view 70 -R macintacos/coroner --json number,title,body,url")

		got, code := runJSON(f, "compute", "minor", "--sha", "HEAD")

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("ok", false))
		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "github_error")))
	})

	DescribeTable("rejects bad usage with exit 2",
		func(args ...string) {
			got, code := runJSON(newFake(), args...)

			Expect(code).To(Equal(2))
			Expect(got).To(HaveKeyWithValue("ok", false))
			Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "usage")))
		},
		Entry("a bad bump", "compute", "bogus", "--sha", "HEAD"),
		Entry("a missing --sha", "compute", "minor"),
		Entry("an unknown subcommand", "bogus"),
		Entry("no subcommand"),
	)
})

var _ = Describe("stability", func() {
	DescribeTable("reads the pull request template's stability line",
		func(body, want string) {
			Expect(stability(body)).To(Equal(want))
		},
		Entry("breaking", "- [ ] No contract change, or `stability:` breaking", "breaking"),
		Entry("additive in backticks", "- [ ] No contract change, or `stability:` `additive`", "additive"),
		Entry("additive", "- [ ] No contract change, or stability: additive", "additive"),
		Entry("experimental in backticks", "- [ ] No contract change, or `stability: experimental`", "experimental"),
		Entry("experimental", "- [ ] No contract change, or `stability:` experimental (`breaking`)", "experimental"),
		Entry("breaking in backticks", "- [ ] `Stability:` `breaking`", "breaking"),
		Entry("unfilled", "- [ ] No contract change, or `stability:` ___ (`breaking`, `additive` or `experimental`)", "unfilled"),
		Entry("none when the box is checked", "- [x] No contract change, or `stability:` ___ (`breaking`, `additive` or `experimental`)", "none"),
		Entry("missing", "Just a description.", "missing"),
	)
})
