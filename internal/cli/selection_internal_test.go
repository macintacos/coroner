package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

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

	run := func(ctx SpecContext, args ...string) {
		GinkgoHelper()
		a := app
		a.Args = append([]string{"--db", db}, args...)
		Expect(Run(ctx, a)).To(Equal(0), strings.Join(args, " "))
	}

	nameEach := func(timelines []timeline.Timeline) []string {
		var named []string
		for _, t := range timelines {
			named = append(named, t.Name)
		}
		return named
	}

	// selected returns the names of the timelines in, env or the current timeline select, and
	// the layer that selected them.
	selected := func(ctx context.Context, in timelineInput, env map[string]string) ([]string, selectedBy, error) {
		sel, err := selectTimelines(ctx, s, in, envOf(env))
		if err != nil || sel.by == "" {
			return nil, sel.by, err
		}
		timelines, err := sel.all(ctx, s)
		return nameEach(timelines), sel.by, err
	}

	// required returns the error a command that needs a timeline fails with when no layer
	// selects one.
	required := func(ctx context.Context, env map[string]string) error {
		sel, err := selectTimelines(ctx, s, timelineInput{}, envOf(env))
		if err != nil {
			return err
		}
		_, err = sel.one(ctx, s)
		return err
	}

	deleteTimelines := func(ctx SpecContext) {
		for _, name := range []string{"alpha", "beta", "gamma"} {
			run(ctx, "timeline", "delete", name, "--yes")
		}
	}

	BeforeEach(func(ctx SpecContext) {
		ids := 0
		home := GinkgoT().TempDir()
		db = filepath.Join(home, "coroner.db")
		app = scratchApp(home, io.Discard, io.Discard, sequentialIDs(&ids))
		app.In = strings.NewReader("")
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
			func(ctx SpecContext, in timelineInput, env map[string]string, noCurrent bool, want []string, wantBy selectedBy) {
				if noCurrent {
					run(ctx, "timeline", "use", "--clear")
				}

				got, by, err := selected(ctx, in, env)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(want))
				Expect(by).To(Equal(wantBy))
			},
			Entry("the flag over the variable and the stored timeline", timelineInput{names: []string{"alpha"}},
				map[string]string{"CORONER_TIMELINE": "beta"}, false, []string{"alpha"}, selectedByFlag),
			Entry("the variable over the stored timeline",
				timelineInput{}, map[string]string{"CORONER_TIMELINE": "beta"}, false, []string{"beta"}, selectedByEnv),
			Entry("the stored timeline under an empty variable",
				timelineInput{}, map[string]string{"CORONER_TIMELINE": ""}, false, []string{"gamma"}, selectedByUse),
			Entry("nothing when no layer names one", timelineInput{}, nil, true, []string(nil), selectedBy("")),
			Entry("nothing under guard, past the variable and the stored timeline",
				timelineInput{rule: timelineRule{guard: true}}, map[string]string{"CORONER_TIMELINE": "beta"}, false,
				[]string(nil), selectedBy("")),
		)

		DescribeTable("never falls through a layer naming a missing timeline",
			func(ctx SpecContext, flag []string, env map[string]string, message string) {
				_, _, err := selected(ctx, timelineInput{names: flag}, env)

				Expect(err).To(HaveField("Kind", KindTimelineNotFound))
				Expect(err).To(HaveField("Message", message))
			},
			Entry("-t", []string{"missing"}, map[string]string{"CORONER_TIMELINE": "beta"}, `-t "missing": no such timeline`),
			Entry("CORONER_TIMELINE", nil, map[string]string{"CORONER_TIMELINE": "missing"},
				`CORONER_TIMELINE="missing": no such timeline`),
		)

		It("reads one timeline for a name and its id given together on a command that reads several",
			func(ctx SpecContext) {
				got, _, err := selected(ctx, timelineInput{names: []string{"alpha", alphaID}}, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal([]string{"alpha"}))
			})

		It("reads an overlay's timelines in its order", func(ctx SpecContext) {
			got, _, err := selected(ctx, timelineInput{overlay: new("o")}, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]string{"beta", "alpha"}))
		})

		It("fails timeline_not_found on an overlay whose timelines were all deleted", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "delta")
			run(ctx, "overlay", "create", "gone", "-t", "delta")
			run(ctx, "timeline", "delete", "delta", "--yes")

			_, _, err := selected(ctx, timelineInput{overlay: new("gone")}, nil)

			Expect(err).To(HaveField("Kind", KindTimelineNotFound))
			Expect(err).To(HaveField("Message", "overlay `gone` reads no timelines: each one was deleted"))
		})

		It("fails overlay_not_found", func(ctx SpecContext) {
			_, _, err := selected(ctx, timelineInput{overlay: new("missing")}, nil)

			Expect(err).To(HaveField("Kind", KindOverlayNotFound))
			Expect(err).To(HaveField("Message", `no overlay is named "missing"`))
		})

		DescribeTable("timeline_required hints",
			func(ctx SpecContext, empty bool, more int, want string) {
				run(ctx, "timeline", "use", "--clear")
				if empty {
					deleteTimelines(ctx)
				}
				for i := range more {
					run(ctx, "timeline", "create", fmt.Sprintf("t%02d", i))
				}

				err := required(ctx, nil)

				failure, ok := errors.AsType[*Error](err)
				Expect(ok).To(BeTrue(), "%v", err)
				Expect(failure.Kind).To(Equal(KindTimelineRequired))
				Expect(failure.Message).To(Equal("no timeline selected"))
				first, _, _ := strings.Cut(failure.Hint, "\n")
				Expect(first).To(Equal(want))
			},
			Entry("lists up to 10 timelines", false, 0, "Timelines: alpha, beta, gamma"),
			Entry("counts the rest past 10", false, 9,
				"Timelines: alpha, beta, gamma, t00, t01, t02, t03, t04, t05, t06, and 2 more"),
			Entry("names coroner timeline create when there are none", true, 0,
				"Run 'coroner timeline create <name>' to create one."),
		)

		DescribeTable("names coroner timeline create on an empty database",
			func(ctx SpecContext, args ...string) {
				deleteTimelines(ctx)

				failure := executeError(ctx, app, append([]string{"--db", db}, args...)...)

				Expect(failure.Kind).To(Equal(KindTimelineRequired))
				Expect(failure.Hint).To(Equal("Run 'coroner timeline create <name>' to create one."))
			},
			Entry("event add", "event", "add", "x", "--at", "2026-09-10T14:02:00Z"),
			Entry("annotation add", "annotation", "add", "hi", "--kind", "comment"),
			Entry("event list", "event", "list"),
		)
	})

	DescribeTable("refuses before the store opens",
		func(ctx SpecContext, args []string, message string) {
			failure := flagError(ctx, args)

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
			failure := flagError(ctx, []string{"event", "view", "1", "-t", "alpha", "-t", alphaID})

			Expect(failure.Kind).To(Equal(KindUsage))
		})

	DescribeTable("refuses several timelines for a format that prints one",
		func(ctx SpecContext, args []string, writes bool) {
			file := filepath.Join(filepath.Dir(db), "x.svg")
			if writes {
				args = append(args, "-o", file)
			}

			failure := executeError(ctx, app, append([]string{"--db", db, "-t", "alpha", "-t", "beta"}, args...)...)

			Expect(failure.Kind).To(Equal(KindUsage))
			Expect(file).NotTo(BeAnExistingFile())
		},
		Entry("timeline view --json", []string{"timeline", "view", "--json"}, false),
		Entry("timeline export --format svg", []string{"timeline", "export", "--format", "svg"}, true),
	)

	It("names --overlay when an overlay selects several timelines for a format that prints one", func(ctx SpecContext) {
		file := filepath.Join(filepath.Dir(db), "x.svg")

		failure := executeError(ctx, app, "--db", db, "timeline", "export", "--overlay", "o", "--format", "svg", "-o", file)

		Expect(failure.Kind).To(Equal(KindUsage))
		Expect(failure.Message).To(ContainSubstring("--overlay selects 2"))
		Expect(file).NotTo(BeAnExistingFile())
	})

	It("names in the count hint exactly the commands that read several timelines", func(ctx SpecContext) {
		var several []string
		walkCommands(NewRoot(ctx, app), func(cmd *cobra.Command) {
			if ruleOf(cmd).several {
				several = append(several, "'"+cmd.CommandPath()+"'")
			}
		})
		Expect(several).NotTo(BeEmpty())

		failure := flagError(ctx, []string{"event", "view", "1", "-t", "alpha", "-t", "beta"})

		Expect(regexp.MustCompile(`'coroner [^']+'`).FindAllString(failure.Hint, -1)).To(ConsistOf(several))
	})

	It("refuses svg for several timelines before reading events", func(ctx SpecContext) {
		run(ctx, "event", "add", "deploy", "-t", "alpha")
		run(ctx, "event", "add", "rollback", "-t", "beta")
		raw, err := sql.Open("sqlite", db)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(raw.Close)
		_, err = raw.ExecContext(ctx, "DROP TABLE event")
		Expect(err).NotTo(HaveOccurred())

		failure := executeError(ctx, app, "--db", db, "timeline", "export", "-t", "alpha", "-t", "beta",
			"--format", "svg", "-o", filepath.Join(filepath.Dir(db), "x.svg"))

		Expect(failure.Kind).To(Equal(KindUsage))
	})

	DescribeTable("reads each command's timeline rule from its annotations",
		func(ctx SpecContext, path string, want timelineRule) {
			cmd, _, err := NewRoot(ctx, app).Find(strings.Fields(path))
			Expect(err).NotTo(HaveOccurred())

			want.command = "coroner " + path
			Expect(ruleOf(cmd)).To(Equal(want))
		},
		Entry("event list", "event list", timelineRule{several: true}),
		Entry("timeline export", "timeline export", timelineRule{several: true, oneFormats: []format{formatSVG}}),
		Entry("timeline view", "timeline view",
			timelineRule{several: true, positional: true, oneFormats: []format{formatJSON}}),
		Entry("import", "import", timelineRule{guard: true}),
		Entry("overlay create", "overlay create", timelineRule{several: true}),
		Entry("event view", "event view", timelineRule{}),
	)

	It("reads a write command's timeline field as -t's values, each once", func() {
		Expect(timelineInputOf(fieldValues{"timeline": {"alpha", "beta", "alpha"}})).
			To(Equal(timelineInput{names: []string{"alpha", "beta"}}))
	})
})
