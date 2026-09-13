package viewer_test

import (
	"bytes"
	"errors"
	"regexp"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
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

// loader is a Load that records every filter it gets and answers with the snapshot next holds.
type loader struct {
	filters []store.EventFilter
	next    viewer.Snapshot
	err     error
}

func (l *loader) load(f store.EventFilter) (viewer.Snapshot, error) {
	l.filters = append(l.filters, f)
	return l.next, l.err
}

func open(snapshot viewer.Snapshot, filter store.EventFilter, l *loader) tea.Model {
	m := viewer.New(viewer.Options{
		Snapshot: snapshot,
		Filter:   filter,
		Load:     l.load,
		Location: newYork(),
		Now:      func() time.Time { return utc(12, 21, 30, 0) },
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
			matchGolden("list", screen(open(fixture(), store.EventFilter{}, l)))
		})

		It("expands the cursor event's evidence and comments on enter", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "enter")
			matchGolden("annotations", screen(m))
		})

		It("lists every key on ?", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "?")
			matchGolden("help", screen(m))
		})

		It("says which events the filter keeps", func() {
			l.next.Events = l.next.Events[:3]
			m := open(fixture(), store.EventFilter{}, l)
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
			matchGolden("empty", screen(open(empty, store.EventFilter{}, l)))
		})

		It("says nothing matches when a filter keeps no event", func() {
			empty := fixture()
			empty.Events = []timeline.Event{}
			Expect(screen(open(empty, store.EventFilter{Contains: "zebra"}, l))).To(ContainSubstring("No events match."))
		})
	})

	Describe("moving", func() {
		It("scrolls a short window so the cursor event and the rules above it show", func() {
			m, _ := open(fixture(), store.EventFilter{}, l).Update(tea.WindowSizeMsg{Width: 80, Height: 7})
			m, _ = press(m, "G")

			Expect(screen(m)).To(ContainSubstring("── Sat 2026-09-12"))
			Expect(cursorLine(m)).To(ContainSubstring("#5"))

			m, _ = press(m, "g")
			Expect(cursorLine(m)).To(ContainSubstring("#2"))
			Expect(screen(m)).To(ContainSubstring("── Thu 2026-09-10"))
		})
	})

	Describe("its filter keys", func() {
		initial := store.EventFilter{Sources: []string{"pagerduty"}, Tags: []string{"phase:detect"}, Contains: "primary"}

		It("loads events from any of the sources s is given, keeping the rest of the filter", func() {
			m, _ := press(open(fixture(), initial, l), "s")
			m, cmd := press(typeText(m, "0"), "esc")
			Expect(cmd).To(BeNil())
			m, _ = press(m, "s")
			_, cmd = press(typeText(m, ", mongod, slack"), "enter")
			settle(m, cmd)

			Expect(l.filters).To(Equal([]store.EventFilter{
				{Sources: []string{"pagerduty", "mongod", "slack"}, Tags: []string{"phase:detect"}, Contains: "primary"},
			}))
		})

		It("loads events carrying every tag t is given", func() {
			m, _ := press(open(fixture(), initial, l), "t")
			_, cmd := press(typeText(m, ",election"), "enter")
			settle(m, cmd)

			Expect(l.filters).To(HaveExactElements(HaveField("Tags", []string{"phase:detect", "election"})))
		})

		It("loads events whose text contains what / is given", func() {
			m, _ := press(open(fixture(), initial, l), "/")
			_, cmd := press(typeText(m, " stepped"), "enter")
			settle(m, cmd)

			Expect(l.filters).To(HaveExactElements(HaveField("Contains", "primary stepped")))
		})

		It("clears a filter given an empty value", func() {
			m, _ := press(open(fixture(), initial, l), "s")
			for range len("pagerduty") {
				m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			_, cmd := press(m, "enter")
			settle(m, cmd)

			Expect(l.filters).To(HaveExactElements(HaveField("Sources", BeEmpty())))
		})

		It("loads nothing and names the problem when a tag breaks its grammar", func() {
			m, _ := press(open(fixture(), initial, l), "t")
			m, cmd := press(typeText(m, ",Bad Tag"), "enter")

			Expect(cmd).To(BeNil())
			Expect(screen(m)).To(ContainSubstring(`tag "Bad Tag"`))
		})
	})

	Describe("jumping to a time", func() {
		It("moves the cursor to the first event at or after the time @ is given", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "@")
			m, _ = press(typeText(m, "2026-09-11 00:05"), "enter")

			Expect(cursorLine(m)).To(ContainSubstring("#4"))
		})

		It("moves the cursor to the last event when every event is earlier", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "@")
			m, _ = press(typeText(m, "2026-09-12 12:00"), "enter")

			Expect(cursorLine(m)).To(ContainSubstring("#5"))
		})

		It("shows why a time it cannot read was rejected, leaving the cursor where it was", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "@")
			m, _ = press(typeText(m, "gibberish"), "enter")

			Expect(screen(m)).To(ContainSubstring(`time "gibberish"`))
			Expect(cursorLine(m)).To(ContainSubstring("#2"))
		})
	})

	Describe("refreshing", func() {
		It("reloads under the same filter and keeps the cursor on the same event", func() {
			filter := store.EventFilter{Tags: []string{"phase:detect"}}
			earlier := timeline.Event{
				ID: "0199a1b2-0000-7000-8000-000000000006", Seq: 6, OccurredAt: utc(11, 2, 0, 0),
				Precision: timeline.PrecisionSecond, Text: "Added while open",
			}
			l.next.Events = append([]timeline.Event{earlier}, l.next.Events...)

			m, _ := press(open(fixture(), filter, l), "down")
			m, cmd := press(m, "r")
			m = settle(m, cmd)

			Expect(l.filters).To(Equal([]store.EventFilter{filter}))
			Expect(screen(m)).To(ContainSubstring("Added while open"))
			Expect(cursorLine(m)).To(ContainSubstring("#1"))
		})

		It("shows a failed load and keeps the events it had", func() {
			l.err = errors.New("the database is busy")
			m, cmd := press(open(fixture(), store.EventFilter{}, l), "r")
			m = settle(m, cmd)

			Expect(screen(m)).To(ContainSubstring("the database is busy"))
			Expect(screen(m)).To(ContainSubstring("Primary stepped down"))
		})
	})

	Describe("leaving", func() {
		It("quits on q", func() {
			_, cmd := press(open(fixture(), store.EventFilter{}, l), "q")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.QuitMsg{}))
		})

		It("interrupts on ctrl+c, even while a prompt is open", func() {
			_, cmd := press(open(fixture(), store.EventFilter{}, l), "ctrl+c")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.InterruptMsg{}))

			_, cmd = press(open(fixture(), store.EventFilter{}, l), "/", "ctrl+c")
			Expect(cmd()).To(BeAssignableToTypeOf(tea.InterruptMsg{}))
		})

		It("types q into an open prompt rather than quitting", func() {
			m, _ := press(open(fixture(), store.EventFilter{}, l), "/")
			m, _ = press(m, "q")
			_, cmd := press(m, "enter")

			settle(m, cmd)
			Expect(l.filters).To(HaveExactElements(HaveField("Contains", "q")))
		})
	})
})
