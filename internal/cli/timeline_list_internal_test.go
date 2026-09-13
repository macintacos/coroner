package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

// unreadable is a terminal no spec expects a command to read keys from.
type unreadable struct{}

func (unreadable) Read([]byte) (int, error) {
	Fail("read keys from the terminal")
	return 0, io.EOF
}

var _ = Describe("timeline list on a terminal", func() {
	var (
		out, errOut bytes.Buffer
		app         App
		env         map[string]string
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
		app = App{
			In:  unreadable{},
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

	It("changes nothing when q leaves", func(ctx SpecContext) {
		Expect(pick(ctx, "jq")).To(Equal(0), errOut.String())
		Expect(current(ctx)).To(Equal("mongo-outage"))
	})

	It("exits 130 and changes nothing on ctrl+c", func(ctx SpecContext) {
		Expect(pick(ctx, "j\x03")).To(Equal(130))
		Expect(current(ctx)).To(Equal("mongo-outage"))
	})

	DescribeTable("prints the plain list without reading keys when a format is asked for",
		func(ctx SpecContext, args ...string) {
			a := app
			a.InIsTTY, a.OutIsTTY = true, true

			Expect(run(ctx, a, append([]string{"timeline", "list"}, args...)...)).To(Equal(0), errOut.String())
			Expect(out.String()).To(ContainSubstring("db-2-disk-full"))
		},
		Entry("--json", "--json"),
		Entry("--format text", "--format", "text"),
	)
})
