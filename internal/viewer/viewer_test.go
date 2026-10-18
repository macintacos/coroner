package viewer_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

func utc(day, hour, minute, second int) time.Time {
	return time.Date(2026, 9, day, hour, minute, second, 0, time.UTC)
}

// fixture is a timeline whose events cross two midnights in America/New_York, with gaps above
// and below the viewer's threshold, an approximate event, an interval and a day-precision event.
func fixture() viewer.Snapshot {
	offset := -4 * 60 * 60
	ended := utc(11, 4, 40, 0)
	stepdown := "0199a1b2-0000-7000-8000-000000000002"
	return viewer.Snapshot{
		Timeline: timeline.Timeline{ID: "0199a1b2-0000-7000-8000-000000000000", Name: "mongo-outage", Title: "Primary election storm"},
		Events: []timeline.Event{
			{
				ID: stepdown, Seq: 2, OccurredAt: utc(11, 3, 14, 7), Precision: timeline.PrecisionSecond,
				Source: new("mongod"), Tags: []string{"election", "phase:detect"}, Text: "Primary stepped down",
			},
			{
				ID: "0199a1b2-0000-7000-8000-000000000001", Seq: 1, OccurredAt: utc(11, 4, 1, 0), Precision: timeline.PrecisionMinute,
				Approximate: true, Source: new("slack"), Text: "Pager fires for db-2",
			},
			{
				ID: "0199a1b2-0000-7000-8000-000000000004", Seq: 4, OccurredAt: utc(11, 4, 10, 0), Precision: timeline.PrecisionMinute,
				EndedAt: &ended, Source: new("mongod"), Text: "Replica lag above 30s",
			},
			{
				ID: "0199a1b2-0000-7000-8000-000000000003", Seq: 3, OccurredAt: utc(11, 4, 42, 30), Precision: timeline.PrecisionSecond,
				Text: "Deploy rolled back",
			},
			{
				ID: "0199a1b2-0000-7000-8000-000000000005", Seq: 5, OccurredAt: utc(12, 4, 0, 0), OccurredOffset: &offset,
				Precision: timeline.PrecisionDay, Tags: []string{"phase:review"}, Text: "Postmortem drafted",
			},
		},
		Annotations: []timeline.Annotation{
			{
				EventID: &stepdown, Kind: timeline.AnnotationEvidence, Body: "replSetStepDown called\nterm 12 -> 13",
				Source: new("mongod"), Author: new("julian"),
			},
			{EventID: &stepdown, Kind: timeline.AnnotationComment, Body: "The heartbeat timeout looks too tight", Author: new("julian")},
			{Kind: timeline.AnnotationComment, Body: "On the whole timeline, so never under an event"},
		},
	}
}

func newYork() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	Expect(err).NotTo(HaveOccurred())
	return loc
}

// loader is a Load that records every match it gets and answers with the snapshot next holds.
type loader struct {
	matches []viewer.Match
	next    viewer.Snapshot
	err     error
}

func (l *loader) load(match viewer.Match) (viewer.Snapshot, error) {
	l.matches = append(l.matches, match)
	return l.next, l.err
}

func open(snapshot viewer.Snapshot, match viewer.Match, l *loader) tea.Model {
	m := viewer.New(viewer.Options{
		Snapshot:  snapshot,
		Match:     match,
		Load:      l.load,
		Location:  newYork(),
		ZoneLabel: "America/New_York",
		Now:       func() time.Time { return utc(12, 21, 30, 0) },
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return next
}

func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// press sends each key, and returns the model and the command the last one gave.
func press(m tea.Model, keys ...string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.Update(keyPress(k))
	}
	return m, cmd
}

// typeText sends text one character at a time.
func typeText(m tea.Model, text string) tea.Model {
	for _, r := range text {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// settle runs cmd and hands its message back to m.
func settle(m tea.Model, cmd tea.Cmd) tea.Model {
	GinkgoHelper()
	Expect(cmd).NotTo(BeNil())
	m, _ = m.Update(cmd())
	return m
}

// screen is m's view with every escape sequence stripped, as a no-color terminal shows it.
func screen(m tea.Model) string {
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.NoTTY}
	_, err := w.Write([]byte(m.View().Content))
	Expect(err).NotTo(HaveOccurred())
	return b.String()
}

func cursorLine(m tea.Model) string {
	return regexp.MustCompile(`(?m)^›.*$`).FindString(screen(m))
}

var _ = Describe("the viewer", func() {
	var l *loader

	BeforeEach(func() {
		l = &loader{next: fixture()}
	})

	Describe("its screen", func() {
		It("lists events with gaps, day breaks and zone-labelled times", func() {
			matchGolden("list", screen(open(fixture(), viewer.Match{}, l)))
		})

		It("expands the cursor event's evidence and comments on enter", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "enter")
			matchGolden("annotations", screen(m))
		})

		It("lists every key on ?", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "?")
			matchGolden("help", screen(m))
		})

		It("says which events the filter keeps", func() {
			l.next.Events = l.next.Events[:3]
			m := open(fixture(), viewer.Match{}, l)
			m, _ = press(m, "s")
			m, cmd := press(typeText(m, "mongod, slack"), "enter")
			m = settle(m, cmd)
			m, _ = press(m, "t")
			m, cmd = press(typeText(m, "election"), "enter")
			matchGolden("filtered", screen(settle(m, cmd)))
		})

		It("says how to add the first event when the timeline has none", func() {
			empty := fixture()
			empty.Events, empty.Annotations = nil, nil
			matchGolden("empty", screen(open(empty, viewer.Match{}, l)))
		})

		It("keeps the evidence mark on each line of a body with carriage returns", func() {
			crlf := fixture()
			crlf.Annotations[0].Body = "replSetStepDown called\r\nterm 12 -> 13\r"
			m, _ := press(open(crlf, viewer.Match{}, l), "enter")

			Expect(screen(m)).To(ContainSubstring("▌ evidence  replSetStepDown called  (from mongod, by julian)"))
			Expect(screen(m)).To(ContainSubstring("▌           term 12 -> 13  "))
		})

		It("never sends a control sequence stored in event text to the terminal", func() {
			hostile := fixture()
			hostile.Events[0].Text = "Primary stepped down\x1b]52;c;aGk=\x07\x1b[2J"
			m := open(hostile, viewer.Match{}, l)

			Expect(m.View().Content).NotTo(ContainSubstring("\x1b]52"))
			Expect(m.View().Content).NotTo(ContainSubstring("\x1b[2J"))
		})

		It("shows a coarse time in its own offset when the viewer's zone would cut its window elsewhere", func() {
			tokyo, india := 9*60*60, 5*60*60+30*60
			coarse := fixture()
			coarse.Events = []timeline.Event{
				{
					ID: "0199a1b2-0000-7000-8000-000000000011", Seq: 1, OccurredAt: utc(11, 15, 0, 0), OccurredOffset: &tokyo,
					Precision: timeline.PrecisionDay, Text: "Recorded in Tokyo",
				},
				{
					ID: "0199a1b2-0000-7000-8000-000000000012", Seq: 2, OccurredAt: utc(12, 4, 30, 0), OccurredOffset: &india,
					Precision: timeline.PrecisionHour, Text: "Recorded in India",
				},
			}
			got := screen(open(coarse, viewer.Match{}, l))

			Expect(got).To(ContainSubstring("── Sat 2026-09-12"))
			Expect(got).NotTo(ContainSubstring("2026-09-11"))
			Expect(got).To(MatchRegexp(`#1\s+--:-- \+09:00\s.*Recorded in Tokyo`))
			Expect(got).To(MatchRegexp(`#2\s+10:-- \+05:30\s.*Recorded in India`))
		})

		It("says nothing matches when a filter keeps no event", func() {
			empty := fixture()
			empty.Events = []timeline.Event{}
			Expect(screen(open(empty, viewer.Match{Contains: "zebra"}, l))).To(ContainSubstring("No events match."))
		})
	})

	Describe("moving", func() {
		It("scrolls a short window so the cursor event and the rules above it show", func() {
			m, _ := open(fixture(), viewer.Match{}, l).Update(tea.WindowSizeMsg{Width: 80, Height: 7})
			m, _ = press(m, "G")

			Expect(screen(m)).To(ContainSubstring("── Sat 2026-09-12"))
			Expect(cursorLine(m)).To(ContainSubstring("#5"))

			m, _ = press(m, "g")
			Expect(cursorLine(m)).To(ContainSubstring("#2"))
			Expect(screen(m)).To(ContainSubstring("── Thu 2026-09-10"))
		})
	})

	Describe("its filter keys", func() {
		initial := viewer.Match{Sources: []string{"pagerduty"}, Tags: []string{"phase:detect"}, Contains: "primary"}

		It("loads events from any of the sources s is given, keeping the rest of the filter", func() {
			m, _ := press(open(fixture(), initial, l), "s")
			m, cmd := press(typeText(m, "0"), "esc")
			Expect(cmd).To(BeNil())
			m, _ = press(m, "s")
			_, cmd = press(typeText(m, ", mongod, slack"), "enter")
			settle(m, cmd)

			Expect(l.matches).To(Equal([]viewer.Match{
				{Sources: []string{"pagerduty", "mongod", "slack"}, Tags: []string{"phase:detect"}, Contains: "primary"},
			}))
		})

		It("loads events carrying every tag t is given", func() {
			m, _ := press(open(fixture(), initial, l), "t")
			_, cmd := press(typeText(m, ",election"), "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Tags", []string{"phase:detect", "election"})))
		})

		It("loads events whose text contains what / is given", func() {
			m, _ := press(open(fixture(), initial, l), "/")
			_, cmd := press(typeText(m, " stepped"), "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Contains", "primary stepped")))
		})

		It("matches the text / is given as typed, spaces included", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "/")
			_, cmd := press(typeText(m, " primary "), "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Contains", " primary ")))
		})

		It("clears the text filter given only spaces", func() {
			m, _ := press(open(fixture(), initial, l), "/")
			for range len("primary") {
				m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			_, cmd := press(typeText(m, "  "), "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Contains", "")))
		})

		It("names each tag t is given once", func() {
			m, _ := press(open(fixture(), initial, l), "t")
			_, cmd := press(typeText(m, ", election, phase:detect,election"), "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Tags", []string{"phase:detect", "election"})))
		})

		It("clears a filter given an empty value", func() {
			m, _ := press(open(fixture(), initial, l), "s")
			for range len("pagerduty") {
				m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			_, cmd := press(m, "enter")
			settle(m, cmd)

			Expect(l.matches).To(HaveExactElements(HaveField("Sources", BeEmpty())))
		})
	})

	Describe("loading", func() {
		It("builds a filter on the one still loading", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "s")
			m, _ = press(typeText(m, "mongod"), "enter")
			m, _ = press(m, "t")
			_, cmd := press(typeText(m, "election"), "enter")
			cmd()

			Expect(l.matches).To(Equal([]viewer.Match{{Sources: []string{"mongod"}, Tags: []string{"election"}}}))
		})

		It("shows the newest load when an older one finishes after it", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "s")
			m, first := press(typeText(m, "mongod"), "enter")
			m, _ = press(m, "t")
			m, second := press(typeText(m, "election"), "enter")
			newer, older := second(), first()
			m, _ = m.Update(newer)
			m, _ = m.Update(older)

			Expect(screen(m)).To(ContainSubstring("from mongod, tagged election"))
		})

		It("builds the next filter on the events showing when a load fails", func() {
			l.err = errors.New(`source "Mongod" is not valid`)
			m, _ := press(open(fixture(), viewer.Match{}, l), "s")
			m, cmd := press(typeText(m, "Mongod"), "enter")
			m = settle(m, cmd)
			m, _ = press(m, "t")
			_, cmd = press(typeText(m, "election"), "enter")
			cmd()

			Expect(l.matches).To(HaveExactElements(HaveField("Sources", ConsistOf("Mongod")), Equal(viewer.Match{Tags: []string{"election"}})))
		})

		It("keeps the cursor where it moved while a load was running", func() {
			m, cmd := press(open(fixture(), viewer.Match{}, l), "r")
			m, _ = press(m, "down")

			Expect(cursorLine(settle(m, cmd))).To(ContainSubstring("#1"))
		})
	})

	Describe("jumping to a time", func() {
		It("moves the cursor to the first event at or after the time @ is given", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "@")
			m, _ = press(typeText(m, "2026-09-11 00:05"), "enter")

			Expect(cursorLine(m)).To(ContainSubstring("#4"))
		})

		It("reads a time pasted at @", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "@")
			m, _ = m.Update(tea.PasteMsg{Content: "2026-09-11 00:05"})
			m, _ = press(m, "enter")

			Expect(cursorLine(m)).To(ContainSubstring("#4"))
		})

		It("moves the cursor to the last event when every event is earlier", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "@")
			m, _ = press(typeText(m, "2026-09-12 12:00"), "enter")

			Expect(cursorLine(m)).To(ContainSubstring("#5"))
		})

		It("shows why a time it cannot read was rejected, leaving the cursor where it was", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "@")
			m, _ = press(typeText(m, "gibberish"), "enter")

			Expect(screen(m)).To(ContainSubstring(`time "gibberish"`))
			Expect(cursorLine(m)).To(ContainSubstring("#2"))
		})
	})

	Describe("refreshing", func() {
		It("reloads under the same filter and keeps the cursor on the same event", func() {
			match := viewer.Match{Tags: []string{"phase:detect"}}
			earlier := timeline.Event{
				ID: "0199a1b2-0000-7000-8000-000000000006", Seq: 6, OccurredAt: utc(11, 2, 0, 0),
				Precision: timeline.PrecisionSecond, Text: "Added while open",
			}
			l.next.Events = append([]timeline.Event{earlier}, l.next.Events...)

			m, _ := press(open(fixture(), match, l), "down")
			m, cmd := press(m, "r")
			m = settle(m, cmd)

			Expect(l.matches).To(Equal([]viewer.Match{match}))
			Expect(screen(m)).To(ContainSubstring("Added while open"))
			Expect(cursorLine(m)).To(ContainSubstring("#1"))
		})

		It("shows a failed load and keeps the events it had", func() {
			l.err = errors.New("the database is busy")
			m, cmd := press(open(fixture(), viewer.Match{}, l), "r")
			m = settle(m, cmd)

			Expect(screen(m)).To(ContainSubstring("the database is busy"))
			Expect(screen(m)).To(ContainSubstring("Primary stepped down"))
		})

		It("wraps a failure too long for one line, keeping its hint and the window's height", func() {
			l.err = errors.New(`tag "Election": must be at most 64 lowercase letters, digits or "._:/-", ` +
				"starting with a letter or digit. Did you mean `election`?")
			m, cmd := press(open(fixture(), viewer.Match{}, l), "r")
			m = settle(m, cmd)

			Expect(screen(m)).To(ContainSubstring("Did you mean `election`?"))
			Expect(strings.Split(screen(m), "\n")).To(HaveLen(20))
		})
	})

	Describe("running on a terminal", func() {
		run := func(ctx context.Context, keys string) error {
			in, typed := io.Pipe()
			DeferCleanup(typed.Close)
			go func() { _, _ = typed.Write([]byte(keys)) }()

			return viewer.Run(ctx, viewer.Terminal{In: in, Out: io.Discard}, viewer.Options{
				Snapshot: fixture(), Load: l.load, Location: newYork(), ZoneLabel: "America/New_York",
				Now: func() time.Time { return utc(12, 21, 30, 0) },
			})
		}

		It("returns ErrInterrupted on ctrl+c", func() {
			Expect(run(context.Background(), "\x03")).To(MatchError(viewer.ErrInterrupted))
		})

		// A panic takes the same path as a cancelled context, and bubbletea prints its stack
		// straight to stderr, so the context stands in for it here.
		It("returns every other end of the program as an error, never as an interruption", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := run(ctx, "")

			Expect(err).To(MatchError(context.Canceled))
			Expect(err).NotTo(MatchError(viewer.ErrInterrupted))
		})
	})

	Describe("leaving", func() {
		It("quits on q", func() {
			_, cmd := press(open(fixture(), viewer.Match{}, l), "q")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.QuitMsg{}))
		})

		It("interrupts on ctrl+c, even while a prompt is open", func() {
			_, cmd := press(open(fixture(), viewer.Match{}, l), "ctrl+c")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.InterruptMsg{}))

			_, cmd = press(open(fixture(), viewer.Match{}, l), "/", "ctrl+c")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.InterruptMsg{}))
		})

		It("types q into an open prompt rather than quitting", func() {
			m, _ := press(open(fixture(), viewer.Match{}, l), "/")
			m, _ = press(m, "q")
			_, cmd := press(m, "enter")

			settle(m, cmd)
			Expect(l.matches).To(HaveExactElements(HaveField("Contains", "q")))
		})
	})
})
