package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

// unreadable is a terminal no spec expects a command to read keys from. It records a read
// rather than failing, since the read happens on bubbletea's input goroutine.
type unreadable struct {
	read atomic.Bool
}

func (u *unreadable) Read([]byte) (int, error) {
	u.read.Store(true)
	return 0, io.EOF
}

var _ = Describe("timeline list on a terminal", func() {
	var (
		out, errOut bytes.Buffer
		app         App
		env         map[string]string
		terminal    *unreadable
	)

	run := func(ctx SpecContext, a App, args ...string) int {
		GinkgoHelper()
		out.Reset()
		errOut.Reset()
		a.Args = args
		return Run(ctx, a)
	}

	// pick runs timeline list on a terminal that types keys.
	pick := func(ctx SpecContext, keys string, args ...string) int {
		GinkgoHelper()
		in, typed := io.Pipe()
		DeferCleanup(typed.Close)
		go func() { _, _ = typed.Write([]byte(keys)) }()
		a := app
		a.In, a.InIsTTY, a.OutIsTTY = in, true, true
		return run(ctx, a, append([]string{"timeline", "list"}, args...)...)
	}

	current := func(ctx SpecContext) string {
		GinkgoHelper()
		Expect(run(ctx, app, "timeline", "list", "--json")).To(Equal(0), errOut.String())
		var listed struct {
			Timelines []struct {
				Name    string `json:"name"`
				Current bool   `json:"current"`
			} `json:"timelines"`
		}
		Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed())
		for _, t := range listed.Timelines {
			if t.Current {
				return t.Name
			}
		}
		return ""
	}

	BeforeEach(func(ctx SpecContext) {
		ids := 0
		home := GinkgoT().TempDir()
		env = map[string]string{"HOME": home, "CORONER_DB": filepath.Join(home, "coroner.db")}
		terminal = &unreadable{}
		app = App{
			In:  terminal,
			Out: &out, Err: &errOut,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist },
			Now:      func() time.Time { return time.Date(2026, time.September, 12, 21, 30, 0, 0, time.UTC) },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore:         store.Open,
			OpenStoreReadOnly: store.OpenReadOnly,
		}

		for _, args := range [][]string{
			{"timeline", "create", "mongo-outage"},
			{"timeline", "create", "db-2-disk-full"},
			{"timeline", "use", "mongo-outage"},
			{"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z", "-t", "db-2-disk-full"},
		} {
			Expect(run(ctx, app, args...)).To(Equal(0), errOut.String())
		}
	})

	It("makes the timeline picked with enter current, as timeline use does", func(ctx SpecContext) {
		Expect(pick(ctx, "j\r")).To(Equal(0), errOut.String())

		Expect(run(ctx, app, "status", "--json")).To(Equal(0), errOut.String())
		Expect(out.String()).To(MatchRegexp(`"name":\s*"db-2-disk-full"`))
		Expect(out.String()).To(MatchRegexp(`"selected_by":\s*"use"`))
	})

	It("stores the pick and warns that CORONER_TIMELINE still selects its timeline", func(ctx SpecContext) {
		env["CORONER_TIMELINE"] = "mongo-outage"

		Expect(pick(ctx, "j\r")).To(Equal(0), errOut.String())

		Expect(errOut.String()).To(ContainSubstring(`CORONER_TIMELINE="mongo-outage"`))
		Expect(current(ctx)).To(Equal("db-2-disk-full"))
	})

	It("makes the timeline picked current when it is renamed while the picker waits", func(ctx SpecContext) {
		in, typed := io.Pipe()
		DeferCleanup(typed.Close)
		renamer := app
		renamer.Out, renamer.Err = io.Discard, io.Discard
		renamer.Args = []string{"timeline", "edit", "db-2-disk-full", "--name", "db-2-disk-full-renamed"}
		go func() {
			_, _ = typed.Write([]byte("j"))
			Run(ctx, renamer)
			_, _ = typed.Write([]byte("\r"))
		}()
		a := app
		a.In, a.InIsTTY, a.OutIsTTY = in, true, true

		Expect(run(ctx, a, "timeline", "list")).To(Equal(0), errOut.String())
		Expect(current(ctx)).To(Equal("db-2-disk-full-renamed"))
	})

	It("changes nothing when q leaves", func(ctx SpecContext) {
		Expect(pick(ctx, "jq")).To(Equal(0), errOut.String())
		Expect(current(ctx)).To(Equal("mongo-outage"))
	})

	It("exits 130 and changes nothing on ctrl+c", func(ctx SpecContext) {
		Expect(pick(ctx, "j\x03")).To(Equal(130))
		Expect(current(ctx)).To(Equal("mongo-outage"))
	})

	DescribeTable("prints the plain list without reading keys when a format is asked for or input is turned off",
		func(ctx SpecContext, noInputEnv string, args ...string) {
			env["CORONER_NO_INPUT"] = noInputEnv
			a := app
			a.InIsTTY, a.OutIsTTY = true, true

			Expect(run(ctx, a, append([]string{"timeline", "list"}, args...)...)).To(Equal(0), errOut.String())
			Expect(out.String()).To(ContainSubstring("db-2-disk-full"))
			Expect(terminal.read.Load()).To(BeFalse())
		},
		Entry("--json", "", "--json"),
		Entry("--format text", "", "--format", "text"),
		Entry("--no-input", "", "--no-input"),
		Entry("CORONER_NO_INPUT", "1"),
	)

	It("prints the plain hint without reading keys when there are no timelines", func(ctx SpecContext) {
		env["CORONER_DB"] = filepath.Join(GinkgoT().TempDir(), "empty.db")
		a := app
		a.InIsTTY, a.OutIsTTY = true, true

		Expect(run(ctx, a, "timeline", "list")).To(Equal(0), errOut.String())
		Expect(errOut.String()).To(ContainSubstring("No timelines yet"))
		Expect(terminal.read.Load()).To(BeFalse())
	})

	It("reads each timeline's event count and last event for the picker", func(ctx SpecContext) {
		for _, args := range [][]string{
			{"timeline", "create", "api-latency"},
			{"event", "add", "p99 latency alert", "--at", "2026-09-11T10:05:00Z", "-t", "api-latency"},
			{"event", "add", "Deploy rolled back", "--at", "2026-09-11T10:30:00Z", "-t", "api-latency"},
			{"event", "add", "Deploy started", "--at", "2026-09-11T09:55:00Z", "-t", "api-latency"},
		} {
			Expect(run(ctx, app, args...)).To(Equal(0), errOut.String())
		}
		s, err := store.Open(ctx, env["CORONER_DB"])
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		rows, err := readTimelineRows(ctx, s)

		Expect(err).NotTo(HaveOccurred())
		type summary struct {
			name   string
			events int
			last   string
		}
		summaries := make([]summary, 0, len(rows))
		for _, row := range rows {
			last := ""
			if row.Last != nil {
				last = row.Last.Text
			}
			summaries = append(summaries, summary{row.Timeline.Name, row.EventCount, last})
		}
		Expect(summaries).To(Equal([]summary{
			{"mongo-outage", 0, ""},
			{"db-2-disk-full", 1, "Primary stepped down"},
			{"api-latency", 3, "Deploy rolled back"},
		}))
	})
})
