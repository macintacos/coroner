package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("timeline selection", func() {
	const alphaID = "0199a1b2-0000-7000-8000-000000000001"

	var (
		app App
		db  string
		s   *store.Store
	)

	lookup := func(env map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		}
	}

	run := func(ctx SpecContext, args ...string) {
		GinkgoHelper()
		a := app
		a.Args = append([]string{"--db", db}, args...)
		Expect(Run(ctx, a)).To(Equal(0), strings.Join(args, " "))
	}

	// execute runs args through the root and returns the error it fails with.
	execute := func(ctx context.Context, a App, args ...string) *Error {
		GinkgoHelper()
		root := NewRoot(ctx, a)
		root.SetArgs(args)
		_, err := root.ExecuteContextC(ctx)
		failure, ok := errors.AsType[*Error](err)
		Expect(ok).To(BeTrue(), "coroner %s returned %v", strings.Join(args, " "), err)
		return failure
	}

	// storeless is app failing the spec if anything opens the store.
	storeless := func() App {
		a := app
		a.OpenStore = func(context.Context, string) (*store.Store, error) {
			Fail("coroner opened the store")
			return nil, errors.New("unreachable")
		}
		a.OpenStoreReadOnly = a.OpenStore
		return a
	}

	nameEach := func(timelines []timeline.Timeline) []string {
		var named []string
		for _, t := range timelines {
			named = append(named, t.Name)
		}
		return named
	}

	// selected returns the names of the timelines names, overlay, env or the current timeline
	// select, and the layer that selected them.
	selected := func(ctx context.Context, names []string, overlay *string, env map[string]string) ([]string, selectedBy, error) {
		switch {
		case overlay != nil:
			timelines, err := selectChosenTimelines(ctx, s, timelineChoice{names: names, overlay: overlay}, lookup(env))
			return nameEach(timelines), "", err
		case len(names) > 1:
			timelines, err := selectTimelines(ctx, s, names, lookup(env))
			return nameEach(timelines), selectedByFlag, err
		}

		var flag *string
		if len(names) == 1 {
			flag = &names[0]
		}
		t, by, err := lookupTimeline(ctx, s, flag, lookup(env))
		if err != nil || by == "" {
			return nil, by, err
		}
		return []string{t.Name}, by, nil
	}

	// required returns the error a command that needs a timeline fails with when no layer
	// selects one.
	required := func(ctx context.Context, env map[string]string) error {
		_, err := selectTimeline(ctx, s, nil, lookup(env))
		return err
	}

	BeforeEach(func(ctx SpecContext) {
		ids := 0
		home := GinkgoT().TempDir()
		db = filepath.Join(home, "coroner.db")
		app = App{
			In: strings.NewReader(""), Out: io.Discard, Err: io.Discard,
			LookupEnv: lookup(map[string]string{"HOME": home}),
			ReadFile:  func(string) ([]byte, error) { return nil, fs.ErrNotExist },
			Now:       testNow,
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore:         store.Open,
			OpenStoreReadOnly: store.OpenReadOnly,
		}
		for _, args := range [][]string{
			{"timeline", "create", "alpha"},
			{"timeline", "create", "beta"},
			{"timeline", "create", "gamma"},
			{"timeline", "use", "gamma"},
			{"overlay", "create", "o", "-t", "beta", "-t", "alpha"},
		} {
			run(ctx, args...)
		}

		var err error
		s, err = store.Open(ctx, db)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
	})

	Describe("against the store", func() {
		DescribeTable("selects by the first layer that names a timeline",
			func(ctx SpecContext, flag []string, env map[string]string, noCurrent bool, want []string, wantBy selectedBy) {
				if noCurrent {
					run(ctx, "timeline", "use", "--clear")
				}

				got, by, err := selected(ctx, flag, nil, env)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(want))
				Expect(by).To(Equal(wantBy))
			},
			Entry("the flag over the variable and the stored timeline",
				[]string{"alpha"}, map[string]string{"CORONER_TIMELINE": "beta"}, false, []string{"alpha"}, selectedByFlag),
			Entry("the variable over the stored timeline",
				nil, map[string]string{"CORONER_TIMELINE": "beta"}, false, []string{"beta"}, selectedByEnv),
			Entry("the stored timeline under an empty variable",
				nil, map[string]string{"CORONER_TIMELINE": ""}, false, []string{"gamma"}, selectedByUse),
			Entry("nothing when no layer names one", nil, nil, true, []string(nil), selectedBy("")),
		)

		DescribeTable("never falls through a layer naming a missing timeline",
			func(ctx SpecContext, flag []string, env map[string]string, message string) {
				_, _, err := selected(ctx, flag, nil, env)

				Expect(err).To(HaveField("Kind", KindTimelineNotFound))
				Expect(err).To(HaveField("Message", message))
			},
			Entry("-t", []string{"missing"}, map[string]string{"CORONER_TIMELINE": "beta"}, `-t "missing": no such timeline`),
			Entry("CORONER_TIMELINE", nil, map[string]string{"CORONER_TIMELINE": "missing"},
				`CORONER_TIMELINE="missing": no such timeline`),
		)

		It("reads one timeline for a name and its id given together on a command that reads several",
			func(ctx SpecContext) {
				got, _, err := selected(ctx, []string{"alpha", alphaID}, nil, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal([]string{"alpha"}))
			})

		It("reads an overlay's timelines in its order", func(ctx SpecContext) {
			got, _, err := selected(ctx, nil, new("o"), nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"beta", "alpha"}))
		})

		It("fails timeline_not_found on an overlay whose timelines were all deleted", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "delta")
			run(ctx, "overlay", "create", "gone", "-t", "delta")
			run(ctx, "timeline", "delete", "delta", "--yes")

			_, _, err := selected(ctx, nil, new("gone"), nil)

			Expect(err).To(HaveField("Kind", KindTimelineNotFound))
			Expect(err).To(HaveField("Message", "overlay `gone` reads no timelines: each one was deleted"))
		})

		It("fails overlay_not_found", func(ctx SpecContext) {
			_, _, err := selected(ctx, nil, new("missing"), nil)

			Expect(err).To(HaveField("Kind", KindOverlayNotFound))
			Expect(err).To(HaveField("Message", `no overlay is named "missing"`))
		})

		DescribeTable("timeline_required hints",
			func(ctx SpecContext, more int, want string) {
				run(ctx, "timeline", "use", "--clear")
				for i := range more {
					run(ctx, "timeline", "create", fmt.Sprintf("t%02d", i))
				}

				err := required(ctx, nil)

				Expect(err).To(HaveField("Kind", KindTimelineRequired))
				Expect(err).To(HaveField("Hint", HavePrefix(want+"\n")))
			},
			Entry("lists up to 10 timelines", 0, "Timelines: alpha, beta, gamma"),
			Entry("counts the rest past 10", 9,
				"Timelines: alpha, beta, gamma, t00, t01, t02, t03, t04, t05, t06, and 2 more"),
		)
	})

	DescribeTable("refuses before the store opens",
		func(ctx SpecContext, args []string, message string) {
			failure := execute(ctx, storeless(), args...)

			Expect(failure.Kind).To(Equal(KindUsage))
			Expect(failure.Message).To(Equal(message))
		},
		Entry("an empty -t", []string{"event", "view", "1", "-t", ""}, "-t must not be empty"),
		Entry("an empty --overlay", []string{"event", "list", "--overlay", ""}, "--overlay must not be empty"),
		Entry("--overlay with -t", []string{"event", "list", "--overlay", "o", "-t", "alpha"},
			"--overlay and -t both select timelines"),
		Entry("a timeline argument with -t", []string{"timeline", "view", "alpha", "-t", "beta"},
			"pass either a timeline name or id, or -t"),
		Entry("two timelines on a command that reads one", []string{"event", "view", "1", "-t", "alpha", "-t", "beta"},
			"-t is given 2 different values, and coroner event view acts on one timeline"),
	)

	It("counts a name and its id as two values on a command that reads one, before the store opens",
		func(ctx SpecContext) {
			failure := execute(ctx, storeless(), "event", "view", "1", "-t", "alpha", "-t", "01a09862-b1c0-7000-8000-000000000001")

			Expect(failure.Kind).To(Equal(KindUsage))
		})

	DescribeTable("refuses several timelines for a format that prints one",
		func(ctx SpecContext, args []string, writes bool) {
			file := filepath.Join(filepath.Dir(db), "x.svg")
			if writes {
				args = append(args, "-o", file)
			}

			failure := execute(ctx, app, append([]string{"--db", db, "-t", "alpha", "-t", "beta"}, args...)...)

			Expect(failure.Kind).To(Equal(KindUsage))
			Expect(file).NotTo(BeAnExistingFile())
		},
		Entry("timeline view --json", []string{"timeline", "view", "--json"}, false),
		Entry("timeline export --format svg", []string{"timeline", "export", "--format", "svg"}, true),
	)
})
