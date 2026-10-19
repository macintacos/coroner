package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	runListCmd  = "gh run list -R macintacos/coroner --workflow release.yml --commit " + headSHA + " --event push --json databaseId,headBranch,url"
	runWatchCmd = "gh run watch 42 -R macintacos/coroner --exit-status --interval 15"
	runViewCmd  = "gh run view 42 -R macintacos/coroner --json status,conclusion,url"
	runURL      = "https://github.com/macintacos/coroner/actions/runs/42"
)

// watchFake lists the tag's release run from the given try on, alongside another tag's run,
// and answers that run as successful.
func watchFake(appearsOn int) *fakeSystem {
	f := newFake()
	f.answers["git rev-parse --verify HEAD^{commit}"] = headSHA
	tries := 0
	f.respond = func(name string, args []string) (string, bool) {
		if name+" "+strings.Join(args, " ") != runListCmd {
			return "", false
		}
		tries++
		if appearsOn == 0 || tries < appearsOn {
			return `[{"databaseId":7,"headBranch":"v0.0.9","url":"other"}]`, true
		}

		return fmt.Sprintf(`[{"databaseId":7,"headBranch":"v0.0.9","url":"other"},{"databaseId":42,"headBranch":%q,"url":%q}]`, rcTag, runURL), true
	}
	f.answers[runViewCmd] = `{"status":"completed","conclusion":"success","url":"` + runURL + `"}`

	return f
}

var _ = Describe("watch", func() {
	It("waits for the tag's run to appear, watches it, and prints its URL", func() {
		f := watchFake(3)

		got, code := runJSON(f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(0))
		Expect(got).To(Equal(map[string]any{"ok": true, "tag": rcTag, "run_url": runURL}))
		Expect(f.sleeps).To(Equal([]time.Duration{15 * time.Second, 15 * time.Second}))
		Expect(f.calls).To(ContainElement(runWatchCmd))
	})

	It("gives up when the run never appears", func() {
		f := watchFake(0)

		got, code := runJSON(f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "workflow_not_found")))
		Expect(f.calls).To(HaveEach(Not(Equal(runWatchCmd))))
		Expect(f.calls).To(HaveLen(41))
	})

	It("fails with the run's URL when the run does not succeed", func() {
		f := watchFake(1)
		f.failing[runWatchCmd] = errFailed
		f.answers[runViewCmd] = `{"status":"completed","conclusion":"failure","url":"` + runURL + `"}`

		got, code := runJSON(f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("error", And(
			HaveKeyWithValue("code", "workflow_failed"),
			HaveKeyWithValue("message", ContainSubstring(runURL)),
		)))
	})

	It("reports an interrupted watch while the run is still going", func() {
		f := watchFake(1)
		f.failing[runWatchCmd] = errFailed
		f.answers[runViewCmd] = `{"status":"in_progress","conclusion":"","url":"` + runURL + `"}`

		got, code := runJSON(f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "watch_interrupted")))
	})

	It("succeeds when the watch fails but the run completed successfully", func() {
		f := watchFake(1)
		f.failing[runWatchCmd] = errFailed

		got, code := runJSON(f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(0))
		Expect(got).To(HaveKeyWithValue("ok", true))
	})

	It("times out when watching the run passes its deadline", func() {
		f := watchFake(1)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		got, code := runJSONContext(ctx, f, "watch", "--tag", rcTag, "--sha", "HEAD")

		Expect(code).To(Equal(1))
		Expect(got).To(HaveKeyWithValue("error", HaveKeyWithValue("code", "workflow_timeout")))
	})
})
