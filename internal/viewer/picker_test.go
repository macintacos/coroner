package viewer_test

import (
	"context"
	"io"
	"regexp"
	"time"

	tea "charm.land/bubbletea/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

// pickerRows lists three timelines, the second of them current.
func pickerRows() []viewer.TimelineRow {
	events := fixture().Events
	return []viewer.TimelineRow{
		{
			Timeline: timeline.Timeline{
				ID: "0199a1b2-0000-7000-8000-000000000010", Name: "mongo-outage", Title: "Primary election storm",
				Refs: []string{"INC-4821", "https://jira.example.com/browse/INC-4821"},
			},
			Events: 5, Last: &events[4],
		},
		{
			Timeline: timeline.Timeline{ID: "0199a1b2-0000-7000-8000-000000000020", Name: "db-2-disk-full"},
			Current:  true,
		},
		{
			Timeline: timeline.Timeline{
				ID: "0199a1b2-0000-7000-8000-000000000030", Name: "checkout-rollback", Title: "Checkout deploy rolled back",
				Refs: []string{"INC-4830"},
			},
			Events: 1, Last: &events[1],
		},
	}
}

func openPicker(notice string) tea.Model {
	m, _ := viewer.NewPicker(viewer.PickOptions{Rows: pickerRows(), Notice: notice, Location: newYork()}).
		Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	return m
}

// messages runs cmd and the commands of every batch it returns, and returns their messages.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, messages(c)...)
	}
	return msgs
}

// drain hands m every message cmd produces, and then those of the commands m returns.
func drain(m tea.Model, cmd tea.Cmd) tea.Model {
	for _, msg := range messages(cmd) {
		var next tea.Cmd
		m, next = m.Update(msg)
		m = drain(m, next)
	}
	return m
}

// pickedName is the name of the timeline m picked, or "" when it picked none.
func pickedName(m tea.Model) string {
	GinkgoHelper()
	p, ok := m.(viewer.Picker)
	Expect(ok).To(BeTrue())
	t, _ := p.Picked()
	return t.Name
}

var _ = Describe("the timeline picker", func() {
	DescribeTable("a row's fields: marker, name, event count, last event, title and refs",
		func(row viewer.TimelineRow, want []string) {
			Expect(viewer.PickerFields(row, newYork())).To(Equal(want))
		},
		Entry("marks the current timeline", pickerRows()[1],
			[]string{"*", "db-2-disk-full", "0 events", "-", "-", "-"}),
		Entry("joins refs and counts events", pickerRows()[0],
			[]string{" ", "mongo-outage", "5 events", "last 2026-09-12 --:-- EDT", "Primary election storm", "INC-4821, https://jira.example.com/browse/INC-4821"}),
		Entry("counts one event and marks an approximate last event", pickerRows()[2],
			[]string{" ", "checkout-rollback", "1 event", "last 2026-09-11 ~00:01 EDT", "Checkout deploy rolled back", "INC-4830"}),
		Entry("keeps a title on one line with no control characters",
			viewer.TimelineRow{Timeline: timeline.Timeline{Name: "x", Title: "Primary\n\tstepped\x1b[2J down"}},
			[]string{" ", "x", "0 events", "-", "Primary stepped[2J down", "-"}),
	)

	Describe("its screen", func() {
		It("lists every timeline with the cursor on the current one", func() {
			matchGolden("picker", screen(openPicker("")))
		})

		It("says which layer selects the timeline commands act on", func() {
			matchGolden("picker-notice", screen(openPicker(`CORONER_TIMELINE="mongo-outage" selects the timeline commands act on`)))
		})
	})

	cursorRow := func(m tea.Model) string {
		return regexp.MustCompile(`(?m)^›.*$`).FindString(screen(m))
	}

	It("opens with the cursor on the current timeline", func() {
		Expect(cursorRow(openPicker(""))).To(MatchRegexp(`\*\s+db-2-disk-full`))
	})

	It("picks the timeline under the cursor on enter", func() {
		m, cmd := press(openPicker(""), "down", "enter")

		Expect(cmd()).To(BeAssignableToTypeOf(tea.QuitMsg{}))
		Expect(pickedName(m)).To(Equal("checkout-rollback"))
	})

	It("narrows the rows to the name or title typed after /", func() {
		m, cmd := press(openPicker(""), "/")
		m = drain(m, cmd)
		for _, r := range "election" {
			m, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			m = drain(m, cmd)
		}
		m, _ = press(m, "enter")

		Expect(screen(m)).NotTo(ContainSubstring("db-2-disk-full"))
		m, _ = press(m, "enter")
		Expect(pickedName(m)).To(Equal("mongo-outage"))
	})

	It("types q into the filter rather than quitting", func() {
		m, cmd := press(openPicker(""), "/")
		m = drain(m, cmd)
		_, cmd = press(m, "q")

		Expect(messages(cmd)).NotTo(ContainElement(BeAssignableToTypeOf(tea.QuitMsg{})))
	})

	DescribeTable("leaves without picking",
		func(key string, want any) {
			m, cmd := press(openPicker(""), key)

			Expect(cmd()).To(BeAssignableToTypeOf(want))
			Expect(pickedName(m)).To(BeEmpty())
		},
		Entry("on q", "q", tea.QuitMsg{}),
		Entry("on esc", "esc", tea.QuitMsg{}),
		Entry("interrupting on ctrl+c", "ctrl+c", tea.InterruptMsg{}),
	)

	Describe("running on a terminal", func() {
		pick := func(keys string) (timeline.Timeline, bool, error) {
			in, typed := io.Pipe()
			DeferCleanup(typed.Close)
			go func() { _, _ = typed.Write([]byte(keys)) }()

			return viewer.Pick(context.Background(), viewer.Terminal{In: in, Out: io.Discard},
				viewer.PickOptions{Rows: pickerRows(), Location: time.UTC})
		}

		It("returns the timeline picked with enter", func() {
			t, ok, err := pick("\r")

			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(t.Name).To(Equal("db-2-disk-full"))
		})

		It("returns nothing picked on q", func() {
			_, ok, err := pick("q")

			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})

		It("returns ErrInterrupted on ctrl+c", func() {
			_, _, err := pick("\x03")
			Expect(err).To(MatchError(viewer.ErrInterrupted))
		})
	})
})
