package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type ran struct {
	code        int
	out, errOut string
}

// kind is the error kind named by the JSON error envelope that ends ran's stderr, or "" when it
// exited 0.
func (r ran) kind() Kind {
	GinkgoHelper()
	if r.code == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(r.errOut), "\n")
	var e envelope
	Expect(json.Unmarshal([]byte(lines[len(lines)-1]), &e)).To(Succeed(), r.errOut)
	return e.Error.Kind
}

// realRunner returns a runner of commands against a database file in a fresh home, with real ids
// and the fixed clock.
func realRunner(ctx context.Context) func(args ...string) ran {
	home := GinkgoT().TempDir()
	db := filepath.Join(home, "coroner.db")
	return func(args ...string) ran {
		var out, errOut bytes.Buffer
		app := scratchApp(home, &out, &errOut, uuid.NewV7)
		app.Args, app.In = append([]string{"--db", db}, args...), strings.NewReader("")
		code := Run(ctx, app)
		return ran{code: code, out: out.String(), errOut: errOut.String()}
	}
}

// atOnce runs a and b concurrently through run.
func atOnce(run func(args ...string) ran, a, b []string) (ran, ran) {
	var ranA, ranB ran
	var wg sync.WaitGroup
	wg.Go(func() { ranA = run(a...) })
	wg.Go(func() { ranB = run(b...) })
	wg.Wait()
	return ranA, ranB
}

var _ = Describe("a write racing another", func() {
	const rounds = 20

	It("gives timeline edit racing timeline delete exit 0 or timeline_not_found, never 1", func(ctx SpecContext) {
		for range rounds {
			run := realRunner(ctx)
			created := run("timeline", "create", "outage", "-q")
			Expect(created.code).To(Equal(0), created.errOut)
			id := strings.TrimSpace(created.out)

			edited, deleted := atOnce(run,
				[]string{"timeline", "edit", id, "--name", "outage-2", "--json"},
				[]string{"timeline", "delete", id, "--yes", "--json"})

			Expect(deleted.code).To(Equal(0), deleted.errOut)
			if edited.code != 0 {
				Expect(edited.code).To(Equal(3), edited.errOut)
				Expect(edited.kind()).To(Equal(KindTimelineNotFound))
			}
		}
	})

	It("gives overlay create racing timeline delete exit 0 or timeline_not_found, never 1", func(ctx SpecContext) {
		for range rounds {
			run := realRunner(ctx)
			seeded := run("timeline", "create", "outage")
			Expect(seeded.code).To(Equal(0), seeded.errOut)
			created := run("timeline", "create", "latency", "-q")
			Expect(created.code).To(Equal(0), created.errOut)
			id := strings.TrimSpace(created.out)

			overlaid, deleted := atOnce(run,
				[]string{"overlay", "create", "o", "-t", "outage", "-t", "latency", "--json"},
				[]string{"timeline", "delete", id, "--yes", "--json"})

			Expect(deleted.code).To(Equal(0), deleted.errOut)
			if overlaid.code != 0 {
				Expect(overlaid.code).To(Equal(3), overlaid.errOut)
				Expect(overlaid.kind()).To(Equal(KindTimelineNotFound))
			}
		}
	})

	It("gives timeline use racing timeline delete exit 0 or timeline_not_found, never 1", func(ctx SpecContext) {
		for range rounds {
			run := realRunner(ctx)
			created := run("timeline", "create", "outage", "-q")
			Expect(created.code).To(Equal(0), created.errOut)
			id := strings.TrimSpace(created.out)

			used, deleted := atOnce(run,
				[]string{"timeline", "use", id, "--json"},
				[]string{"timeline", "delete", id, "--yes", "--json"})

			Expect(deleted.code).To(Equal(0), deleted.errOut)
			if used.code != 0 {
				Expect(used.code).To(Equal(3), used.errOut)
				Expect(used.kind()).To(Equal(KindTimelineNotFound))
			}
		}
	})

	It("lets one of two renames of a source win, and gives the other source_not_found", func(ctx SpecContext) {
		for range rounds {
			run := realRunner(ctx)
			seeded := run("timeline", "create", "outage")
			Expect(seeded.code).To(Equal(0), seeded.errOut)
			seeded = run("event", "add", "Election timeout on db-3", "-t", "outage", "--source", "mongo",
				"--at", "2026-09-11T03:14:30Z")
			Expect(seeded.code).To(Equal(0), seeded.errOut)

			a, b := atOnce(run,
				[]string{"source", "edit", "mongo", "--name", "a", "--json"},
				[]string{"source", "edit", "mongo", "--name", "b", "--json"})

			Expect([]int{a.code, b.code}).To(ConsistOf(0, 3), a.errOut+b.errOut)
			Expect([]Kind{a.kind(), b.kind()}).To(ConsistOf(Kind(""), KindSourceNotFound))
		}
	})
})
