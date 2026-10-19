package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("timeline", func() {
	var (
		out, errOut bytes.Buffer
		home        string
		env         map[string]string
		now         time.Time
		ids         int
	)

	newApp := func(args ...string) cli.App {
		return harness{
			home: home, db: filepath.Join(home, "coroner.db"), env: env, out: &out, errOut: &errOut,
			now:   func() time.Time { return now },
			newID: countedIDs(&ids),
		}.app(args...)
	}

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, newApp(args...))
	}

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		env = map[string]string{"HOME": home}
		now = time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
		ids = 0
	})

	It("prints its help when run without a verb", func(ctx SpecContext) {
		Expect(run(ctx, "timeline")).To(Equal(0))

		Expect(out.String()).To(ContainSubstring("coroner timeline <command>"))
	})

	It("rejects a verb it does not have with exit 2 and a suggestion", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "craete")).To(Equal(2))

		Expect(errOut.String()).To(ContainSubstring(`unknown command "craete" for "coroner timeline"`))
		Expect(errOut.String()).To(MatchRegexp(`Did you mean this\?\n\s+create`))
	})

	Describe("create", func() {
		It("prints the created timeline as JSON and says what changed on stderr", func(ctx SpecContext) {
			code := run(ctx, "timeline", "create", "mongo-outage", "--title", "Primary election storm",
				"--ref", "INC-4821", "--ref", "https://example.com/INC-4821", "--json")

			Expect(code).To(Equal(0))
			Expect(out.String()).To(MatchJSON(`{
				"id": "0199a1b2-0000-7000-8000-000000000001",
				"name": "mongo-outage",
				"title": "Primary election storm",
				"refs": ["INC-4821", "https://example.com/INC-4821"],
				"created_at": "2026-09-11T03:00:00.000000000Z"
			}`))
			Expect(errOut.String()).To(ContainSubstring("mongo-outage"))
			Expect(errOut.String()).To(ContainSubstring("coroner timeline use mongo-outage"))
		})

		It("keeps a repeated ref once, in the order first given", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-outage", "--ref", "INC-2", "--ref", "INC-1", "--ref", "INC-2", "--json")).
				To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring(`"refs":["INC-2","INC-1"]`))
		})

		It("rejects an empty ref with exit 2", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-outage", "--ref", "")).To(Equal(2))
		})

		DescribeTable("exits 2 with kind usage on a flag that is not UTF-8, creating nothing",
			func(ctx SpecContext, flag, value string) {
				Expect(run(ctx, "timeline", "create", "mongo-outage", "--json", flag, value)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
				Expect(run(ctx, "timeline", "export", "-t", "mongo-outage", "--format", "jsonl")).To(Equal(3))
			},
			Entry(nil, "--title", "Primary election storm \xff"),
			Entry(nil, "--ref", "INC-4821\xff"),
		)

		It("leaves the current timeline alone", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "first")).To(Equal(0), errOut.String())
			Expect(run(ctx, "timeline", "use", "first")).To(Equal(0), errOut.String())
			Expect(run(ctx, "timeline", "create", "second")).To(Equal(0), errOut.String())

			Expect(run(ctx, "timeline", "list")).To(Equal(0), errOut.String())

			Expect(out.String()).To(MatchRegexp(`(?m)^\*\tfirst\t`))
		})
	})

	Describe("list", func() {
		BeforeEach(func(ctx SpecContext) {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			Expect(run(ctx, "timeline", "create", "zeta", "--title", "oldest")).To(Equal(0), errOut.String())
			now = now.Add(time.Minute)
			Expect(run(ctx, "timeline", "create", "alpha")).To(Equal(0), errOut.String())
			Expect(run(ctx, "timeline", "create", "beta", "--title", "same instant, higher id")).To(Equal(0), errOut.String())
		})

		It("prints one row per timeline in creation order, with no marker while none is in use", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "list")).To(Equal(0))

			Expect(out.String()).To(Equal("\tzeta\toldest\n\talpha\t\n\tbeta\tsame instant, higher id\n"))
		})

		It("marks the timeline in use", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "alpha")).To(Equal(0), errOut.String())

			Expect(run(ctx, "timeline", "list")).To(Equal(0), errOut.String())

			Expect(out.String()).To(Equal("\tzeta\toldest\n*\talpha\t\n\tbeta\tsame instant, higher id\n"))
		})
	})

	It("lists a title holding a tab and a newline on one line", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "create", "mongo-outage", "--title", "election\tstorm\nday 2")).To(Equal(0), errOut.String())

		Expect(run(ctx, "timeline", "list")).To(Equal(0))

		Expect(out.String()).To(Equal("\tmongo-outage\telection\\tstorm\\nday 2\n"))
	})

	It("lists nothing and hints at create when there are no timelines", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0))
		Expect(out.String()).To(Equal(`{"timelines":[]}` + "\n"))

		Expect(run(ctx, "timeline", "list")).To(Equal(0))
		Expect(out.String()).To(BeEmpty())
		Expect(errOut.String()).To(ContainSubstring("coroner timeline create"))
	})

	Describe("on a timeline with events and an annotation", func() {
		const timelineID = "0199a1b2-0000-7000-8000-000000000001"

		addEvent := func(ctx SpecContext, s *store.Store, n int, at time.Time, source, text string, tags ...string) {
			GinkgoHelper()
			_, err := s.AddEvent(ctx, timeline.Event{
				ID:         fmt.Sprintf("0199a1b2-0000-7000-8000-1000000000%02d", n),
				TimelineID: timelineID,
				OccurredAt: at,
				Precision:  timeline.PrecisionSecond,
				Source:     &source,
				Tags:       append([]string{}, tags...),
				Text:       text,
				CreatedAt:  now,
				UpdatedAt:  now,
			}, fmt.Sprintf("0199a1b2-0000-7000-8000-2000000000%02d", n))
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func(ctx SpecContext) {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			Expect(run(ctx, "timeline", "create", "mongo-outage", "--title", "Primary election storm", "--ref", "INC-4821")).
				To(Equal(0), errOut.String())

			s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(s.Close)
			// Added out of time order, so seq order and time order differ.
			addEvent(ctx, s, 1, time.Date(2026, 9, 11, 3, 15, 0, 0, time.UTC), "slack", "Pager fires for db-2")
			addEvent(ctx, s, 2, time.Date(2026, 9, 11, 3, 14, 0, 0, time.UTC), "mongod", "Primary stepped down", "election")
			addEvent(ctx, s, 3, time.Date(2026, 9, 11, 4, 20, 30, 0, time.UTC), "mongod", "New primary elected", "election")
			eventID := "0199a1b2-0000-7000-8000-100000000002"
			Expect(s.AddAnnotation(ctx, timeline.Annotation{
				ID: "0199a1b2-0000-7000-8000-300000000001", TimelineID: timelineID, EventID: &eventID,
				Kind: timeline.AnnotationEvidence, Body: "replSetStepDown", CreatedAt: now, UpdatedAt: now,
			}, "0199a1b2-0000-7000-8000-400000000001")).To(Succeed())
		})

		Describe("view", func() {
			It("lists the events in time order, each with the gap since the one before", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(0))

				Expect(out.String()).To(HaveSuffix("\n\n" +
					"#2\t2026-09-11T03:14:00Z\t\tmongod\tPrimary stepped down\n" +
					"#1\t2026-09-11T03:15:00Z\t+1m0s\tslack\tPager fires for db-2\n" +
					"#3\t2026-09-11T04:20:30Z\t+1h5m30s\tmongod\tNew primary elected\n"))
			})

			It("shows times in the --tz zone, with its offset", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "view", "mongo-outage", "--tz", "America/New_York")).To(Equal(0))

				Expect(out.String()).To(MatchRegexp(
					`(?m)^Events: +3, from 2026-09-10T23:14:00-04:00 to 2026-09-11T00:20:30-04:00$`))
				Expect(out.String()).To(ContainSubstring("#2\t2026-09-10T23:14:00-04:00\t"))
			})

			It("summarizes the timeline before its events", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(0), errOut.String())

				Expect(out.String()).To(HavePrefix("mongo-outage\n"))
				Expect(out.String()).To(MatchRegexp(`(?m)^Title: +Primary election storm$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Refs: +INC-4821$`))
				Expect(out.String()).To(MatchRegexp(
					`(?m)^Events: +3, from 2026-09-11T03:14:00Z to 2026-09-11T04:20:30Z$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Sources: +mongod 2, slack 1$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Tags: +election 2$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Annotations: +1$`))
			})

			It("views the current timeline when none is named", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())

				Expect(run(ctx, "timeline", "view")).To(Equal(0))
				Expect(out.String()).To(HavePrefix("mongo-outage\n"))
			})

			It("lists only the events --source, --tag and --contains keep", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "view", "mongo-outage", "--source", "mongod", "--tag", "election", "--contains", "ELECTED")).
					To(Equal(0))

				Expect(out.String()).To(HaveSuffix("\n\n#3\t2026-09-11T04:20:30Z\t\tmongod\tNew primary elected\n"))
			})

			It("counts the annotations on the events it keeps and on the timeline itself", func(ctx SpecContext) {
				s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
				Expect(err).NotTo(HaveOccurred())
				Expect(s.AddAnnotation(ctx, timeline.Annotation{
					ID: "0199a1b2-0000-7000-8000-300000000002", TimelineID: timelineID,
					Kind: timeline.AnnotationComment, Body: "Why did the pager lag?", CreatedAt: now, UpdatedAt: now,
				}, "0199a1b2-0000-7000-8000-400000000002")).To(Succeed())
				Expect(s.Close()).To(Succeed())

				Expect(run(ctx, "timeline", "view", "mongo-outage", "--source", "slack")).To(Equal(0), errOut.String())

				Expect(out.String()).To(MatchRegexp(`(?m)^Annotations: +1$`))
			})

			Describe("on a terminal", func() {
				// altScreen is the escape sequence a full-screen program starts with.
				const altScreen = `\x1b\[\?1049h`

				// viewOn runs timeline view with stdin and stdout on a terminal, reading keys from the
				// returned writer, and sends its exit code on the returned channel.
				viewOn := func(ctx SpecContext, screen io.Writer, args ...string) (io.Writer, <-chan int) {
					keys, typed := io.Pipe()
					app := newApp(append([]string{"timeline", "view", "mongo-outage"}, args...)...)
					app.In, app.Out, app.Err = keys, screen, gbytes.NewBuffer()
					app.InIsTTY, app.OutIsTTY = true, true

					code := make(chan int, 1)
					go func() {
						code <- cli.Run(ctx, app)
						// Unblocks a key write the finished command will never read.
						_ = keys.Close()
					}()
					return typed, code
				}

				It("opens the viewer, where an agent keeps adding events and q exits 0", func(ctx SpecContext) {
					screen := gbytes.NewBuffer()
					keys, code := viewOn(ctx, screen)
					Eventually(screen).Should(gbytes.Say(altScreen))

					Expect(run(ctx, "event", "add", "Replica lag alarm", "-t", "mongo-outage")).To(Equal(0), errOut.String())

					_, err := keys.Write([]byte("q"))
					Expect(err).NotTo(HaveOccurred())
					Eventually(code).Should(Receive(Equal(0)))
				})

				It("exits 130 on ctrl+c", func(ctx SpecContext) {
					screen := gbytes.NewBuffer()
					keys, code := viewOn(ctx, screen)
					Eventually(screen).Should(gbytes.Say(altScreen))

					_, err := keys.Write([]byte{0x03})
					Expect(err).NotTo(HaveOccurred())
					Eventually(code).Should(Receive(Equal(130)))
				})

				It("prints plain text when --format text is given", func(ctx SpecContext) {
					var screen bytes.Buffer
					_, code := viewOn(ctx, &screen, "--format", "text")

					Eventually(code).Should(Receive(Equal(0)))
					Expect(screen.String()).To(HavePrefix("mongo-outage\n"))
					Expect(screen.String()).NotTo(ContainSubstring("\x1b"))
				})

				DescribeTable("prints plain text when TERM is dumb or input is turned off",
					func(ctx SpecContext, vars map[string]string, args ...string) {
						maps.Copy(env, vars)
						var screen bytes.Buffer
						_, code := viewOn(ctx, &screen, args...)

						Eventually(code).Should(Receive(Equal(0)))
						Expect(screen.String()).To(HavePrefix("mongo-outage\n"))
					},
					Entry("TERM=dumb", map[string]string{"TERM": "dumb"}),
					Entry("--no-input", nil, "--no-input"),
					Entry("CORONER_NO_INPUT=1", map[string]string{"CORONER_NO_INPUT": "1"}),
				)

				It("prints plain text when stdin is not a terminal", func(ctx SpecContext) {
					var screen bytes.Buffer
					app := newApp("timeline", "view", "mongo-outage")
					app.Out, app.OutIsTTY = &screen, true

					Expect(cli.Run(ctx, app)).To(Equal(0))
					Expect(screen.String()).To(HavePrefix("mongo-outage\n"))
				})
			})
		})

		Describe("edit", func() {
			It("renames a timeline, keeping its id and its events", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--name", "mongo-failover", "--json")).To(Equal(0))
				Expect(out.String()).To(ContainSubstring(`"id":"` + timelineID + `","name":"mongo-failover"`))
				Expect(errOut.String()).To(ContainSubstring("mongo-failover"))

				Expect(run(ctx, "timeline", "view", "mongo-failover")).To(Equal(0))
				Expect(out.String()).To(ContainSubstring("Primary stepped down"))
				Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(3))
			})

			It("sets the title, and clears it when given an empty one", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--title", "Failover", "--json")).To(Equal(0), errOut.String())
				Expect(out.String()).To(ContainSubstring(`"title":"Failover"`))

				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--title", "", "--json")).To(Equal(0), errOut.String())
				Expect(out.String()).To(ContainSubstring(`"title":""`))
			})

			It("removes refs before adding them", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage",
					"--add-ref", "INC-4822", "--add-ref", "INC-4821", "--remove-ref", "INC-4821", "--json")).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"refs":["INC-4822","INC-4821"]`))
			})

			It("leaves refs alone when adding one it has or removing one it lacks", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage",
					"--add-ref", "INC-4821", "--remove-ref", "INC-9999", "--json")).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"refs":["INC-4821"]`))
			})

			It("rejects an empty --add-ref with exit 2", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--add-ref", "")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring("must not be blank"))
			})

			DescribeTable("exits 2 with kind usage on a flag that is not UTF-8",
				func(ctx SpecContext, flag, value string) {
					Expect(run(ctx, "timeline", "edit", "mongo-outage", "--json", flag, value)).To(Equal(2))

					Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
					expectNothingInvalidStored(ctx, filepath.Join(home, "coroner.db"))
				},
				Entry(nil, "--title", "Failover \xff"),
				Entry(nil, "--add-ref", "INC-4821\xff"),
			)

			It("writes nothing under --dry-run", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--name", "mongo-failover", "--dry-run")).To(Equal(0))
				Expect(out.String()).To(HavePrefix("mongo-failover\n"))

				Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(0))
			})

			It("refuses under --dry-run a name another timeline has with exit 4", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0), errOut.String())

				Expect(run(ctx, "timeline", "edit", "mongo-outage", "--name", "mongo-failover", "--dry-run")).To(Equal(4))
			})
		})

		Describe("delete", func() {
			It("reports what it would delete under --dry-run, deleting nothing", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "delete", "mongo-outage", "--dry-run")).To(Equal(0))
				Expect(out.String()).To(ContainSubstring("events: 3, annotations: 1"))

				Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(0))
			})
		})
	})

	Describe("use", func() {
		BeforeEach(func(ctx SpecContext) {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0), errOut.String())
		})

		It("selects a timeline by id as well as by name", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "0199a1b2-0000-7000-8000-000000000001")).To(Equal(0))

			Expect(run(ctx, "timeline", "list")).To(Equal(0), errOut.String())
			Expect(out.String()).To(HavePrefix("*\tmongo-outage"))
		})

		It("exits 2, naming both timelines, when one's name is the other's id", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "0199a1b2-0000-7000-8000-000000000001")).To(Equal(0), errOut.String())

			Expect(run(ctx, "timeline", "use", "0199a1b2-0000-7000-8000-000000000001")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring("0199a1b2-0000-7000-8000-000000000002"))
			Expect(errOut.String()).To(ContainSubstring("mongo-outage"))
		})

		It("clears the selection with --clear", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0), errOut.String())

			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0))

			Expect(run(ctx, "timeline", "list")).To(Equal(0), errOut.String())
			Expect(out.String()).To(HavePrefix("\tmongo-outage"))
		})
	})

	Describe("selecting the timeline to act on", func() {
		BeforeEach(func(ctx SpecContext) {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			for _, name := range []string{"a", "b", "c"} {
				Expect(run(ctx, "timeline", "create", name)).To(Equal(0), errOut.String())
			}
			Expect(run(ctx, "timeline", "use", "b")).To(Equal(0), errOut.String())
		})

		It("takes CORONER_TIMELINE over the timeline in use, and -t over both", func(ctx SpecContext) {
			env["CORONER_TIMELINE"] = "a"

			Expect(run(ctx, "timeline", "view", "--json")).To(Equal(0))
			Expect(out.String()).To(ContainSubstring(`"name":"a"`))

			Expect(run(ctx, "timeline", "view", "--json", "-t", "c")).To(Equal(0))
			Expect(out.String()).To(ContainSubstring(`"name":"c"`))
		})

		It("exits 3 naming -t when -t names no timeline, even with one in use", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "-t", "missing")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`-t "missing": no such timeline`))
		})

		It("exits 3 naming CORONER_TIMELINE when it names no timeline, even with one in use", func(ctx SpecContext) {
			env["CORONER_TIMELINE"] = "missing"

			Expect(run(ctx, "timeline", "view")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`CORONER_TIMELINE="missing": no such timeline`))
		})

		It("rejects a timeline named both as an argument and with -t with exit 2", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "a", "-t", "c")).To(Equal(2))
		})

		It("exits 2 when nothing selects a timeline, listing the timelines and every way to select one",
			func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0), errOut.String())

				Expect(run(ctx, "timeline", "view")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring("a, b, c"))
				Expect(errOut.String()).To(ContainSubstring("-t"))
				Expect(errOut.String()).To(ContainSubstring("CORONER_TIMELINE"))
				Expect(errOut.String()).To(ContainSubstring("coroner timeline use"))
			})

		It("lists only the first 10 timelines when nothing selects one", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0), errOut.String())
			for i := range 9 {
				Expect(run(ctx, "timeline", "create", fmt.Sprintf("d%d", i))).To(Equal(0), errOut.String())
			}

			Expect(run(ctx, "timeline", "view")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring("Timelines: a, b, c, d0, d1, d2, d3, d4, d5, d6, and 2 more\n"))
		})
	})

	It("rejects an empty -t with exit 2 before creating the database", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "view", "-t", "")).To(Equal(2))

		Expect(filepath.Join(home, "coroner.db")).NotTo(BeAnExistingFile())
	})

	Describe("a command that changes a timeline", func() {
		entries := []TableEntry{
			Entry("create", "second", "timeline", "create", "second"),
			Entry("edit", "mongo-outage", "timeline", "edit", "mongo-outage", "--title", "Failover"),
			Entry("use", "mongo-outage", "timeline", "use", "mongo-outage"),
			Entry("delete", "mongo-outage", "timeline", "delete", "mongo-outage", "--yes"),
		}

		BeforeEach(func(ctx SpecContext) {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0), errOut.String())
		})

		DescribeTable("names the timeline it changed on stderr",
			func(ctx SpecContext, name string, args ...string) {
				Expect(run(ctx, args...)).To(Equal(0))

				Expect(errOut.String()).To(ContainSubstring(name))
			},
			entries,
		)

		DescribeTable("prints nothing on stderr under -q",
			func(ctx SpecContext, _ string, args ...string) {
				Expect(run(ctx, append(args, "-q")...)).To(Equal(0))

				Expect(errOut.String()).To(BeEmpty())
			},
			entries,
		)
	})
})
