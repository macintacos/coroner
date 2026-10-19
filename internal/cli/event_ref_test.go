package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("event view, edit and delete", func() {
	// Ids differ from their eighth digit, so a prefix can name one event or several.
	const (
		steppedDown = "0199a1b4-0000-7000-8000-000000000000"
		pagerFires  = "0199a1b6-0000-7000-8000-000000000000"
		drill       = "0199a1b8-0000-7000-8000-000000000000"
	)

	var (
		out, errOut bytes.Buffer
		home        string
		ids         int
		in          io.Reader
		inIsTTY     bool
		env         map[string]string
		now         time.Time
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, cli.App{
			Args:    append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
			In:      in,
			InIsTTY: inIsTTY,
			Out:     &out,
			Err:     &errOut,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: readNoFile,
			Now:      func() time.Time { return now },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("%08x-0000-7000-8000-000000000000", 0x0199a1b2+ids))
			},
			OpenStore: store.Open,
		})
	}
	withStore := func(ctx SpecContext) *store.Store {
		GinkgoHelper()
		s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		return s
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		ids = 0
		in, inIsTTY = strings.NewReader(""), false
		env = map[string]string{"HOME": home}
		newYork, err := time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())
		now = time.Date(2026, time.September, 12, 21, 30, 0, 0, newYork)
		for _, args := range [][]string{
			{"timeline", "create", "mongo-outage"},
			{"timeline", "use", "mongo-outage"},
			{"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07.412Z", "--source", "mongod", "--tag", "election"},
			{"event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:15:00Z"},
			{"timeline", "create", "mongo-failover"},
			{"event", "add", "Failover drill", "-t", "mongo-failover", "--at", "2026-09-11T05:00:00Z"},
		} {
			Expect(run(ctx, args...)).To(Equal(0), errOut.String())
		}
	})

	Describe("view", func() {
		DescribeTable("finds an event by seq, a timeline's seq, its id or a unique start of its id",
			func(ctx SpecContext, ref, id string) {
				Expect(run(ctx, "event", "view", ref, "--json")).To(Equal(0), errOut.String())

				Expect(out.String()).To(HavePrefix(`{"id":"` + id + `"`))
			},
			Entry("a seq in the current timeline", "2", pagerFires),
			Entry("a seq in a named timeline", "mongo-failover#1", drill),
			Entry("a full id, from any timeline", drill, drill),
			Entry("a unique start of an id, in any case", "0199A1B4", steppedDown),
		)

		It("finds a seq in the timeline -t names", func(ctx SpecContext) {
			Expect(run(ctx, "event", "view", "1", "-t", "mongo-failover", "--json")).To(Equal(0))

			Expect(out.String()).To(HavePrefix(`{"id":"` + drill + `"`))
		})

		DescribeTable("exits 2 with kind usage given -t and anything but a bare seq",
			func(ctx SpecContext, ref string) {
				Expect(run(ctx, "event", "view", ref, "-t", "mongo-failover", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			},
			Entry("an id", steppedDown),
			Entry("a timeline's seq", "mongo-outage#1"),
		)

		It("finds an event by id while CORONER_TIMELINE names a missing timeline", func(ctx SpecContext) {
			env["CORONER_TIMELINE"] = "missing"

			Expect(run(ctx, "event", "view", steppedDown, "--json")).To(Equal(0), errOut.String())
		})

		It("needs no selected timeline for an id, only for a bare seq", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0))

			Expect(run(ctx, "event", "view", steppedDown, "--json")).To(Equal(0))
			Expect(run(ctx, "event", "view", "1", "--json")).To(Equal(2))
			Expect(errOut.String()).To(ContainSubstring(`"kind":"timeline_required"`))
		})

		It("exits 2 with kind ambiguous_id, listing each match as timeline#seq, on a start several ids share",
			func(ctx SpecContext) {
				Expect(run(ctx, "event", "view", "0199a1b", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"ambiguous_id"`))
				Expect(errOut.String()).To(ContainSubstring("mongo-outage#1  " + steppedDown))
				Expect(errOut.String()).To(ContainSubstring("mongo-failover#1  " + drill))
			})

		DescribeTable("exits 3 with kind event_not_found on an event that does not exist",
			func(ctx SpecContext, ref string) {
				Expect(run(ctx, "event", "view", ref, "--json")).To(Equal(3))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
			},
			Entry("a seq past the last", "9"),
			Entry("an id no event has", "0199a1c"),
		)

		It("exits 2 with kind usage on something that is not an event reference", func(ctx SpecContext) {
			Expect(run(ctx, "event", "view", "#2", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
		})

		It("describes the event and lists its annotations in text, with times in the display zone",
			func(ctx SpecContext) {
				eventID := steppedDown
				Expect(withStore(ctx).AddAnnotation(ctx, timeline.Annotation{
					ID: "0199a1c0-0000-7000-8000-000000000000", TimelineID: "0199a1b3-0000-7000-8000-000000000000",
					EventID: &eventID, Kind: timeline.AnnotationEvidence, Body: "replSetStepDown", CreatedAt: now, UpdatedAt: now,
				}, "0199a1c1-0000-7000-8000-000000000000")).To(Succeed())

				Expect(run(ctx, "event", "view", "1")).To(Equal(0))

				Expect(out.String()).To(HavePrefix("Event #1 in timeline `mongo-outage`\n"))
				Expect(out.String()).To(MatchRegexp(`(?m)^Occurred: +2026-09-10T23:14:07\.412-04:00 \(millisecond\)$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Source: +mongod$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Text: +Primary stepped down$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Annotations: +1$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^evidence\t2026-09-12T21:30:00-04:00\t-\t-\treplSetStepDown$`))
			})
	})

	Describe("edit", func() {
		stored := func(ctx SpecContext, id string) timeline.Event {
			GinkgoHelper()
			e, err := withStore(ctx).Event(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			return e
		}

		It("removes tags, adds tags, bumps updated at and keeps created at", func(ctx SpecContext) {
			now = now.Add(time.Hour)

			Expect(run(ctx, "event", "edit", "1", "--add-tag", "root-cause", "--add-tag", "phase:detect",
				"--remove-tag", "election", "--json")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring(`"tags":["phase:detect","root-cause"]`))
			Expect(out.String()).To(ContainSubstring(
				`"created_at":"2026-09-13T01:30:00.000000000Z","updated_at":"2026-09-13T02:30:00.000000000Z"`))
			Expect(stored(ctx, steppedDown)).To(SatisfyAll(
				HaveField("Tags", Equal([]string{"phase:detect", "root-cause"})),
				HaveField("CreatedAt", BeTemporally("==", now.Add(-time.Hour))),
				HaveField("UpdatedAt", BeTemporally("==", now))))
			Expect(errOut.String()).To(ContainSubstring("Edited event #1 in timeline `mongo-outage`"))
		})

		It("prints the edited event under --dry-run and changes nothing", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "1", "--add-tag", "root-cause", "--remove-tag", "election", "--dry-run",
				"--json")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring(`"tags":["root-cause"]`))
			Expect(stored(ctx, steppedDown).Tags).To(Equal([]string{"election"}))
		})

		It("describes the edited event under --dry-run in text, says what it would do, and changes nothing",
			func(ctx SpecContext) {
				before := stored(ctx, steppedDown)

				Expect(run(ctx, "event", "edit", "1", "--at", "2026-09-11T03:01:00Z", "--dry-run")).To(Equal(0), errOut.String())

				Expect(out.String()).To(HavePrefix("Event #1 in timeline `mongo-outage`\n"))
				Expect(errOut.String()).To(ContainSubstring(
					"Would edit event #1 in timeline `mongo-outage` at 2026-09-11T03:01:00Z (-04:00 local: 23:01:00)"))
				Expect(stored(ctx, steppedDown)).To(Equal(before))
			})

		It("reads --at as event add does, replacing the typed time, precision and offset, and says what it resolved to",
			func(ctx SpecContext) {
				Expect(run(ctx, "event", "edit", "1", "--at", "2026-09-11 03:20", "--tz", "Asia/Tokyo", "--json")).
					To(Equal(0), errOut.String())

				Expect(out.String()).To(ContainSubstring(`"occurred_at":"2026-09-10T18:20:00.000000000Z","occurred_offset":32400,` +
					`"precision":"minute","approximate":false,"occurred_raw":"2026-09-11 03:20"`))
				Expect(errOut.String()).To(ContainSubstring("at 2026-09-10T18:20:00Z (+09:00 Asia/Tokyo: 03:20:00)"))
			})

		It("sets the text, URL, author, end and approximate mark", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", pagerFires, "--text", "Pager fires for db-2 and db-3",
				"--url", "https://pagerduty.example.com/i/1", "--author", "julian", "--ended", "2026-09-11T03:20:00Z",
				"--approximate", "--json")).To(Equal(0), errOut.String())

			Expect(stored(ctx, pagerFires)).To(SatisfyAll(
				HaveField("Text", "Pager fires for db-2 and db-3"),
				HaveField("URL", HaveValue(Equal("https://pagerduty.example.com/i/1"))),
				HaveField("Author", HaveValue(Equal("julian"))),
				HaveField("EndedAt", HaveValue(BeTemporally("==", time.Date(2026, 9, 11, 3, 20, 0, 0, time.UTC)))),
				HaveField("Approximate", BeTrue())))
		})

		It("clears the source, URL, author and end given none", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "1", "--url", "https://logs.example.com/l/1", "--author", "julian",
				"--ended", "2026-09-11T03:20:00Z")).To(Equal(0), errOut.String())

			Expect(run(ctx, "event", "edit", "1", "--source", "none", "--url", "none", "--author", "none",
				"--ended", "none")).To(Equal(0), errOut.String())

			Expect(stored(ctx, steppedDown)).To(SatisfyAll(
				HaveField("Source", BeNil()), HaveField("URL", BeNil()), HaveField("Author", BeNil()),
				HaveField("EndedAt", BeNil())))
		})

		It("moves the event to a source, creating it on first use", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "2", "--source", "pagerduty")).To(Equal(0), errOut.String())

			Expect(stored(ctx, pagerFires).Source).To(HaveValue(Equal("pagerduty")))
		})

		It("keeps an explicit --approximate=false over a ~ in --at", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "1", "--at", "~2026-09-11T03:01:00Z", "--approximate=false")).
				To(Equal(0), errOut.String())

			Expect(stored(ctx, steppedDown).Approximate).To(BeFalse())
		})

		It("clears the approximate mark given --approximate=false", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "1", "--approximate")).To(Equal(0), errOut.String())

			Expect(run(ctx, "event", "edit", "1", "--approximate=false")).To(Equal(0), errOut.String())

			Expect(stored(ctx, steppedDown).Approximate).To(BeFalse())
		})

		It("reads the text from stdin given --text -", func(ctx SpecContext) {
			in = strings.NewReader("Primary stepped down: replSetStepDown\n")

			Expect(run(ctx, "event", "edit", "1", "--text", "-")).To(Equal(0), errOut.String())

			Expect(stored(ctx, steppedDown).Text).To(Equal("Primary stepped down: replSetStepDown"))
		})

		It("exits 2 instead of waiting on a terminal given --text -", func(ctx SpecContext) {
			reader, writer := io.Pipe()
			DeferCleanup(writer.Close)
			in, inIsTTY = reader, true

			Expect(run(ctx, "event", "edit", "1", "--text", "-")).To(Equal(2))
		}, SpecTimeout(10*time.Second))

		It("names both times when the event would end before it occurred", func(ctx SpecContext) {
			Expect(run(ctx, "event", "edit", "1", "--ended", "2026-09-11T03:00:00Z")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring("2026-09-10T23:00:00-04:00"))
			Expect(errOut.String()).To(ContainSubstring("2026-09-10T23:14:07.412-04:00"))
		})

		DescribeTable("exits 2 and changes nothing on an edit no event could take",
			func(ctx SpecContext, kind string, args ...string) {
				before := stored(ctx, steppedDown)
				now = now.Add(time.Hour)

				Expect(run(ctx, append([]string{"event", "edit", "1", "--json"}, args...)...)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"` + kind + `"`))
				Expect(stored(ctx, steppedDown)).To(Equal(before))
			},
			Entry("no field to change", "usage"),
			Entry("blank text", "usage", "--text", " "),
			Entry("a blank URL", "usage", "--url", ""),
			Entry("an invalid source", "invalid_name", "--source", "DB 2"),
			Entry("an invalid tag to add", "invalid_name", "--add-tag", "Root Cause"),
			Entry("an end before the event", "invalid_time", "--ended", "2026-09-11T03:00:00Z"),
			Entry("an approximate end", "invalid_time", "--ended", "~2026-09-11T03:20:00Z"),
			Entry("a time it cannot read", "invalid_time", "--at", "the day before"),
			Entry("an offset beyond a day", "invalid_time", "--at", "2026-09-11T03:14:00-24:59"),
			Entry("an offset beyond a day under --dry-run", "invalid_time", "--at", "2026-09-11T03:14:00-24:59",
				"--dry-run"),
		)

		DescribeTable("exits 2 with kind usage on a flag that is not UTF-8, changing nothing",
			func(ctx SpecContext, flag, value string) {
				before := stored(ctx, steppedDown)

				Expect(run(ctx, "event", "edit", "1", "--json", flag, value)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
				Expect(stored(ctx, steppedDown)).To(Equal(before))
				Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
			},
			Entry(nil, "--url", "https://logs.example.com/\xff"),
			Entry(nil, "--author", "julian\xff"),
		)
	})

	Describe("link", func() {
		It("exits 2 with kind usage on an --author that is not UTF-8", func(ctx SpecContext) {
			Expect(run(ctx, "event", "link", "1", "2", "--kind", "led_to", "--author", "julian\xff", "--json")).
				To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
		})

		It("exits 2 with kind usage on a USER author that is not UTF-8, linking nothing", func(ctx SpecContext) {
			env["USER"] = "julian\xff"

			Expect(run(ctx, "event", "link", "1", "2", "--kind", "led_to", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			Expect(errOut.String()).To(ContainSubstring("the default author, from the config file's author or USER, must be UTF-8"))
			Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
			Expect(out.String()).NotTo(ContainSubstring(`"type":"link"`))
		})
	})

	It("exits 2 on event add --source none, saying why none is no source", func(ctx SpecContext) {
		Expect(run(ctx, "event", "add", "x", "--source", "none")).To(Equal(2))

		Expect(errOut.String()).To(ContainSubstring("none clears an event's source in event edit"))
	})

	Describe("delete", func() {
		const outageID = "0199a1b3-0000-7000-8000-000000000000"
		events := func(ctx SpecContext, timelineID string) []timeline.Event {
			GinkgoHelper()
			e, err := withStore(ctx).Events(ctx, timelineID)
			Expect(err).NotTo(HaveOccurred())
			return e
		}

		It("deletes every event given, with its annotations, printing each as it was", func(ctx SpecContext) {
			eventID := steppedDown
			Expect(withStore(ctx).AddAnnotation(ctx, timeline.Annotation{
				ID: "0199a1c0-0000-7000-8000-000000000000", TimelineID: outageID,
				EventID: &eventID, Kind: timeline.AnnotationComment, Body: "Root cause", CreatedAt: now, UpdatedAt: now,
			}, "0199a1c1-0000-7000-8000-000000000000")).To(Succeed())

			Expect(run(ctx, "event", "delete", "1", pagerFires, "--json")).To(Equal(0), errOut.String())

			Expect(out.String()).To(MatchRegexp(
				`^\{"timelines":\[\{"id":"` + outageID + `","name":"mongo-outage",[^{}]*\}\],"events":\[\{"id":"` + steppedDown + `",[^{}]*"text":"Primary stepped down",[^{}]*\},\{"id":"` +
					pagerFires + `",[^{}]*\}\],"truncated":false,"total":2\}\n$`))
			Expect(errOut.String()).To(ContainSubstring("Deleted 2 events from timeline `mongo-outage`"))
			Expect(events(ctx, outageID)).To(BeEmpty())
			Expect(withStore(ctx).Annotations(ctx, outageID)).To(BeEmpty())
		})

		It("deletes nothing and exits 3 with kind event_not_found when any event does not exist", func(ctx SpecContext) {
			Expect(run(ctx, "event", "delete", "1", "9", "--json")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
			Expect(events(ctx, outageID)).To(HaveLen(2))
		})

		It("describes what it would delete under --dry-run, and deletes nothing", func(ctx SpecContext) {
			Expect(run(ctx, "event", "delete", "1", "--dry-run")).To(Equal(0), errOut.String())

			Expect(out.String()).To(HavePrefix("Event #1 in timeline `mongo-outage`\n"))
			Expect(out.String()).To(MatchRegexp(`(?m)^Text: +Primary stepped down$`))
			Expect(errOut.String()).To(ContainSubstring("Would delete 1 event from timeline `mongo-outage`"))
			Expect(events(ctx, outageID)).To(HaveLen(2))
		})

		It("deletes an event named twice once, and events in several timelines", func(ctx SpecContext) {
			Expect(run(ctx, "event", "delete", "1", steppedDown, "mongo-failover#1", "--json")).To(Equal(0), errOut.String())

			Expect(out.String()).To(HaveSuffix(`"total":2}` + "\n"))
			Expect(errOut.String()).To(ContainSubstring("Deleted 1 event from timeline `mongo-outage`"))
			Expect(errOut.String()).To(ContainSubstring("Deleted 1 event from timeline `mongo-failover`"))
			Expect(events(ctx, outageID)).To(HaveLen(1))
		})
	})
})
