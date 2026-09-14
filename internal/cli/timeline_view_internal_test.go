package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/viewer"
)

func loggedAtDebug(hash string) types.GomegaMatcher {
	return And(ContainSubstring("level=DEBUG"), ContainSubstring(hash))
}

var _ = Describe("timelineLoader", func() {
	const timelineID = "0199a1b2-0000-7000-8000-000000000001"

	var (
		out, errOut bytes.Buffer
		app         App
		db          string
		ids         int
	)

	run := func(ctx SpecContext, args ...string) int {
		GinkgoHelper()
		out.Reset()
		errOut.Reset()
		a := app
		a.Args = append([]string{"--db", db}, args...)
		return Run(ctx, a)
	}

	// open returns the viewer on the fixture timeline, reading through timelineLoader.
	open := func(ctx SpecContext) tea.Model {
		GinkgoHelper()
		load := timelineLoader(ctx, app, db, timelineID)
		snapshot, err := load(viewer.Match{})
		Expect(err).NotTo(HaveOccurred())
		return viewer.New(viewer.Options{Snapshot: snapshot, Load: load, Location: time.UTC, ZoneLabel: "UTC", Now: app.Now})
	}

	// submit opens the prompt key names, types text into it and applies what it loads.
	submit := func(m tea.Model, key, text string) tea.Model {
		GinkgoHelper()
		m, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		for _, r := range text {
			m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		Expect(cmd).NotTo(BeNil())
		m, _ = m.Update(cmd())
		return m
	}

	shownSeqs := func(m tea.Model) []int64 {
		seqs := []int64{}
		for _, match := range regexp.MustCompile(`#(\d+)\b`).FindAllStringSubmatch(m.View().Content, -1) {
			seq, err := strconv.ParseInt(match[1], 10, 64)
			Expect(err).NotTo(HaveOccurred())
			seqs = append(seqs, seq)
		}
		return seqs
	}

	listedSeqs := func(ctx SpecContext, flags ...string) []int64 {
		GinkgoHelper()
		Expect(run(ctx, append([]string{"event", "list", "--json"}, flags...)...)).To(Equal(0), errOut.String())
		var page struct {
			Events []struct {
				Seq int64 `json:"seq"`
			} `json:"events"`
		}
		Expect(json.Unmarshal(out.Bytes(), &page)).To(Succeed())
		seqs := []int64{}
		for _, e := range page.Events {
			seqs = append(seqs, e.Seq)
		}
		return seqs
	}

	BeforeEach(func(ctx SpecContext) {
		ids = 0
		home := GinkgoT().TempDir()
		db = filepath.Join(home, "coroner.db")
		app = App{
			Out:       &out,
			Err:       &errOut,
			LookupEnv: func(key string) (string, bool) { return map[string]string{"HOME": home}[key], key == "HOME" },
			ReadFile:  func(string) ([]byte, error) { return nil, fs.ErrNotExist },
			Now:       func() time.Time { return time.Date(2026, time.September, 12, 21, 30, 0, 0, time.UTC) },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore:         store.Open,
			OpenStoreReadOnly: store.OpenReadOnly,
		}

		for _, args := range [][]string{
			{"timeline", "create", "mongo-outage"},
			{"timeline", "use", "mongo-outage"},
			{"event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:00:00Z", "--source", "pagerduty", "--tag", "phase:detect"},
			{
				"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z", "--source", "mongod",
				"--tag", "election", "--tag", "phase:detect",
			},
			{"event", "add", "New primary elected", "--at", "2026-09-11T03:14:07Z", "--source", "mongod", "--tag", "election"},
			{"event", "add", "Deploy rolled back", "--at", "2026-09-11T04:00:00Z", "--source", "slack"},
		} {
			Expect(run(ctx, args...)).To(Equal(0), errOut.String())
		}
	})

	DescribeTable("keeps the events event list keeps with the equivalent flags",
		func(ctx SpecContext, flags []string, prompts [][2]string) {
			m := open(ctx)
			for _, p := range prompts {
				m = submit(m, p[0], p[1])
			}

			Expect(shownSeqs(m)).To(Equal(listedSeqs(ctx, flags...)))
		},
		Entry("any of several sources", []string{"--source", "pagerduty", "--source", "slack"},
			[][2]string{{"s", "pagerduty, slack"}}),
		Entry("every one of several tags", []string{"--tag", "election", "--tag", "phase:detect"},
			[][2]string{{"t", "election, phase:detect"}}),
		Entry("text, ignoring case", []string{"--contains", "PRIMARY"}, [][2]string{{"/", "PRIMARY"}}),
		Entry("text with a leading space", []string{"--contains", " primary"}, [][2]string{{"/", " primary"}}),
		Entry("a source, a tag and text together", []string{"--source", "mongod", "--tag", "election", "--contains", "elected"},
			[][2]string{{"s", "mongod"}, {"t", "election"}, {"/", "elected"}}),
	)

	It("shows a tag's grammar error with its suggestion, keeping the events it showed", func(ctx SpecContext) {
		m, _ := open(ctx).Update(tea.WindowSizeMsg{Width: 400, Height: 24})
		shown := shownSeqs(m)

		m = submit(m, "t", "Election")

		Expect(m.View().Content).To(ContainSubstring(`tag "Election"`))
		Expect(m.View().Content).To(ContainSubstring("Did you mean `election`?"))
		Expect(shownSeqs(m)).To(Equal(shown))
	})

	Describe("attachments", func() {
		var picture []byte

		BeforeEach(func(ctx SpecContext) {
			app.ReadFile = os.ReadFile
			var b bytes.Buffer
			Expect(png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3)))).To(Succeed())
			picture = b.Bytes()
			path := filepath.Join(GinkgoT().TempDir(), "p99.png")
			Expect(os.WriteFile(path, picture, 0o600)).To(Succeed())
			Expect(run(ctx, "event", "add", "Latency spike", "--at", "2026-09-11T03:20:00Z", "--attach", path)).
				To(Equal(0), errOut.String())
		})

		It("reads every attachment on the timeline into what the viewer shows", func(ctx SpecContext) {
			snapshot, err := timelineLoader(ctx, app, db, timelineID)(viewer.Match{})

			Expect(err).NotTo(HaveOccurred())
			Expect(snapshot.Attachments).To(HaveExactElements(HaveField("Filename", "p99.png")))
		})

		DescribeTable("reads an attachment's image from its blob",
			func(ctx SpecContext, protocol graphics.Protocol, png func(picture []byte) types.GomegaMatcher) {
				snapshot, err := timelineLoader(ctx, app, db, timelineID)(viewer.Match{})
				Expect(err).NotTo(HaveOccurred())
				logger, _ := viewerDiagnostics(false, &errOut)

				img, err := imageReader(app.ReadFile, db, protocol, logger)(snapshot.Attachments[0])

				Expect(err).NotTo(HaveOccurred())
				Expect(img).To(And(HaveField("Width", 4), HaveField("Height", 3), HaveField("PNG", png(picture))))
			},
			Entry("decoded for a terminal that shows images", graphics.Kitty, func(picture []byte) types.GomegaMatcher {
				return Equal(picture)
			}),
			Entry("only its size for one that does not", graphics.None, func([]byte) types.GomegaMatcher { return BeNil() }),
		)

		DescribeTable("an image it cannot show",
			func(ctx SpecContext, readFile func(string) ([]byte, error), debug bool, logged func(hash string) types.GomegaMatcher) {
				snapshot, err := timelineLoader(ctx, app, db, timelineID)(viewer.Match{})
				Expect(err).NotTo(HaveOccurred())
				a := snapshot.Attachments[0]
				errOut.Reset()
				logger, flush := viewerDiagnostics(debug, &errOut)

				_, err = imageReader(readFile, db, graphics.Kitty, logger)(a)
				Expect(err).To(HaveOccurred())
				Expect(errOut.String()).To(BeEmpty(), "nothing reaches stderr while the viewer holds the screen")

				Expect(flush()).To(Succeed())
				Expect(errOut.String()).To(logged(a.SHA256))
			},
			Entry("is logged with its hash once the viewer has closed, under --debug, when its blob is unreadable",
				func(string) ([]byte, error) { return nil, fs.ErrNotExist }, true, loggedAtDebug),
			Entry("is logged with its hash under --debug when its blob does not decode",
				func(string) ([]byte, error) { return []byte("GIF89a not really"), nil }, true, loggedAtDebug),
			Entry("is logged nowhere without --debug",
				func(string) ([]byte, error) { return nil, fs.ErrNotExist }, false,
				func(string) types.GomegaMatcher { return BeEmpty() }),
		)
	})

	It("lets a read still running log while the viewer's diagnostics flush", func(ctx SpecContext) {
		logger, flush := viewerDiagnostics(true, &errOut)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range 100 {
				logger.DebugContext(ctx, "attachment image not shown")
			}
		}()

		Expect(flush()).To(Succeed())
		<-done
	})

	It("never creates the database when it is gone before a refresh", func(ctx SpecContext) {
		load := timelineLoader(ctx, app, db, timelineID)
		Expect(os.Remove(db)).To(Succeed())

		_, err := load(viewer.Match{})

		Expect(err).To(HaveOccurred())
		Expect(db).NotTo(BeAnExistingFile())
	})

	It("lets another coroner add an event while it is open, and shows it on refresh", func(ctx SpecContext) {
		m := open(ctx)

		Expect(run(ctx, "event", "add", "Replica lag alarm", "--at", "2026-09-11T03:30:00Z")).To(Equal(0), errOut.String())
		Expect(errOut.String()).NotTo(ContainSubstring("busy"))

		m, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		Expect(cmd).NotTo(BeNil())
		m, _ = m.Update(cmd())

		Expect(m.View().Content).To(ContainSubstring("Replica lag alarm"))
	})
})
