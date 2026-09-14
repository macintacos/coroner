package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
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

var _ = Describe("event add", func() {
	const timelineID = "0199a1b2-0000-7000-8000-000000000001"

	var (
		out, errOut bytes.Buffer
		home        string
		now         time.Time
		ids         int
		in          io.Reader
		inIsTTY     bool
		env         map[string]string
		configFile  string
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
			ReadFile: func(string) ([]byte, error) {
				if configFile == "" {
					return nil, fs.ErrNotExist
				}
				return []byte(configFile), nil
			},
			Now: func() time.Time { return now },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore: store.Open,
		})
	}

	storedEvents := func(ctx SpecContext) []timeline.Event {
		GinkgoHelper()
		s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		events, err := s.Events(ctx, timelineID)
		Expect(err).NotTo(HaveOccurred())
		return events
	}

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		newYork, err := time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())
		// App.Now's zone is the local zone.
		now = time.Date(2026, time.September, 12, 21, 30, 5, 250_000_000, newYork)
		ids = 0
		in = strings.NewReader("")
		inIsTTY = false
		env = map[string]string{"HOME": home, "USER": "julian"}
		configFile = ""
	})

	When("a timeline is current", func() {
		BeforeEach(func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0))
			Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0))
		})

		It("adds the event and prints it as JSON, with no offset for a Z time", func(ctx SpecContext) {
			code := run(ctx, "event", "add", "Primary stepped down: replSetStepDown",
				"--at", "2026-09-11T03:14:07.412Z", "--source", "mongod", "--tag", "election",
				"--url", "https://logs.example.com/l/1", "--json")

			Expect(code).To(Equal(0))
			Expect(out.String()).To(MatchJSON(`{
				"id": "0199a1b2-0000-7000-8000-000000000002",
				"timeline_id": "` + timelineID + `",
				"seq": 1,
				"occurred_at": "2026-09-11T03:14:07.412000000Z",
				"occurred_offset": null,
				"precision": "millisecond",
				"approximate": false,
				"occurred_raw": "2026-09-11T03:14:07.412Z",
				"ended_at": null,
				"source": "mongod",
				"tags": ["election"],
				"text": "Primary stepped down: replSetStepDown",
				"url": "https://logs.example.com/l/1",
				"author": "julian",
				"created_at": "2026-09-13T01:30:05.250000000Z",
				"updated_at": "2026-09-13T01:30:05.250000000Z",
				"attachments": []
			}`))
			Expect(storedEvents(ctx)).To(ConsistOf(SatisfyAll(
				HaveField("Seq", int64(1)),
				HaveField("OccurredOffset", BeNil()),
				HaveField("OccurredAt", BeTemporally("==", time.Date(2026, time.September, 11, 3, 14, 7, 412_000_000, time.UTC))))))
		})

		It("adds to the timeline -t names instead of the current one", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0))

			Expect(run(ctx, "event", "add", "Primary stepped down", "-t", "mongo-failover", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"timeline_id":"0199a1b2-0000-7000-8000-000000000002"`))
			Expect(storedEvents(ctx)).To(BeEmpty())
		})

		It("says on stderr which timeline it added to and the time it resolved", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z")).To(Equal(0))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(Equal(
				"Added event #1 to timeline `mongo-outage` at 2026-09-11T03:14:07Z (-04:00 local: 23:14:07)\n"))
		})

		It("prints nothing on stderr under --quiet", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Primary stepped down", "-q")).To(Equal(0))

			Expect(errOut.String()).To(BeEmpty())
		})

		It("happens now, to the second, when --at is not given", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Primary stepped down", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(
				`"occurred_at":"2026-09-13T01:30:05.000000000Z","occurred_offset":-14400,"precision":"second"`))
			Expect(out.String()).To(ContainSubstring(`"occurred_raw":null`))
		})

		It("reads a time with no offset in --tz", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Failover", "--at", "2026-09-11 03:14", "--tz", "Asia/Tokyo", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(
				`"occurred_at":"2026-09-10T18:14:00.000000000Z","occurred_offset":32400,"precision":"minute"`))
			Expect(errOut.String()).To(ContainSubstring("(+09:00 Asia/Tokyo: 03:14:00)"))
		})

		It("reads a time with no offset in the config file's default_timezone", func(ctx SpecContext) {
			configFile = `default_timezone = "Asia/Tokyo"`

			Expect(run(ctx, "event", "add", "Failover", "--at", "2026-09-11 03:14", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"occurred_offset":32400`))
		})

		It("exits 2 on a zone it does not know", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Failover", "--tz", "Mars/Olympus")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`unknown time zone "Mars/Olympus"`))
		})

		It("exits 2 with kind invalid_time and the offsets to choose from on a time a DST change skips", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Failover", "--at", "2026-03-08 02:30", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_time"`))
			Expect(errOut.String()).To(ContainSubstring("-05:00"))
			Expect(storedEvents(ctx)).To(BeEmpty())
		})

		It("marks the time approximate from --approximate or an around prefix", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Deploy started", "--at", "2026-09-11 14:00", "--approximate", "--json")).
				To(Equal(0))
			Expect(out.String()).To(ContainSubstring(`"approximate":true`))

			Expect(run(ctx, "event", "add", "Deploy started", "--at", "around 2026-09-11 14:00", "--json")).To(Equal(0))
			Expect(out.String()).To(ContainSubstring(`"approximate":true`))
			Expect(errOut.String()).To(ContainSubstring("at ~2026-09-11T18:00:00Z"))
		})

		It("sets the interval end from --ended", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Deploy", "--at", "2026-09-11 14:00", "--ended", "2026-09-11 14:20", "--json")).
				To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"ended_at":"2026-09-11T18:20:00.000000000Z"`))
		})

		It("exits 2 with kind invalid_time when --ended is before --at", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Deploy", "--at", "2026-09-11T14:20:00Z", "--ended", "2026-09-11T14:00:00Z",
				"--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_time"`))
		})

		DescribeTable("exits 2 with kind invalid_time and a hint to use --approximate on an approximate --ended",
			func(ctx SpecContext, ended string) {
				Expect(run(ctx, "event", "add", "Deploy", "--at", "2026-09-11 14:00", "--ended", ended, "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_time"`))
				Expect(errOut.String()).To(ContainSubstring("pass --approximate"))
				Expect(storedEvents(ctx)).To(BeEmpty())
			},
			Entry(nil, "~2026-09-11 14:20"),
			Entry(nil, "around 2026-09-11 14:20"),
		)

		It("exits 2 with kind invalid_time and the years it takes on a time outside them in UTC", func(ctx SpecContext) {
			Expect(run(ctx, "event", "add", "Epoch", "--at", "0000-01-01T00:00:00+01:00", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_time"`))
			Expect(errOut.String()).To(ContainSubstring("0000 to 9999"))
		})

		Describe("--dry-run", func() {
			It("prints the event as it would be added, with no seq yet, and adds nothing", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Deploy started", "--at", "2026-09-11T14:00:00Z", "--dry-run", "--json")).
					To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"seq":null,"occurred_at":"2026-09-11T14:00:00.000000000Z"`))
				Expect(storedEvents(ctx)).To(BeEmpty())
			})

			It("describes the event in text", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Deploy started", "--at", "2026-09-11T14:00:00Z", "--dry-run")).To(Equal(0))

				Expect(out.String()).To(HavePrefix(
					"Would add to timeline `mongo-outage` at 2026-09-11T14:00:00Z (-04:00 local: 10:00:00)\n"))
				Expect(out.String()).To(MatchRegexp(`(?m)^Text: +Deploy started$`))
				Expect(storedEvents(ctx)).To(BeEmpty())
			})
		})

		Describe("its text", func() {
			It("comes from stdin given -, losing one trailing newline", func(ctx SpecContext) {
				in = strings.NewReader("replSetStepDown\n\n")

				Expect(run(ctx, "event", "add", "-", "--json")).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"text":"replSetStepDown\n"`))
			})

			It("is never read from stdin unless given -", func(ctx SpecContext) {
				// An open stdin that never closes, as agent harnesses attach.
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in = reader

				Expect(run(ctx, "event", "add", "Primary stepped down")).To(Equal(0))
			}, SpecTimeout(10*time.Second))

			It("exits 2 with a hint instead of waiting on a terminal given -", func(ctx SpecContext) {
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in, inIsTTY = reader, true

				Expect(run(ctx, "event", "add", "-")).To(Equal(2))
				Expect(errOut.String()).To(ContainSubstring("Pipe the text in"))
			}, SpecTimeout(10*time.Second))

			It("is read from stdin only once the flags are valid", func(ctx SpecContext) {
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in = reader

				Expect(run(ctx, "event", "add", "-", "--tz", "Mars/Olympus")).To(Equal(2))
			}, SpecTimeout(10*time.Second))

			It("exits 2 when blank", func(ctx SpecContext) {
				in = strings.NewReader("\n")

				Expect(run(ctx, "event", "add", "-")).To(Equal(2))
				Expect(errOut.String()).To(ContainSubstring("Pass non-blank text."))
			})

			It("exits 2 with kind usage when not UTF-8", func(ctx SpecContext) {
				in = strings.NewReader("Primary stepped down \xff")

				Expect(run(ctx, "event", "add", "-", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
				Expect(errOut.String()).To(ContainSubstring("UTF-8"))
				Expect(storedEvents(ctx)).To(BeEmpty())
			})

			It("exits 2 when split over several arguments, hinting to quote it", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary", "stepped", "down")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring("Quote"))
			})
		})

		Describe("its source", func() {
			It("is created the first time an event names it", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--source", "db-2/mongod")).To(Equal(0))
				Expect(run(ctx, "event", "add", "New primary elected", "--source", "db-2/mongod")).To(Equal(0))

				s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(s.Close)
				Expect(s.Sources(ctx)).To(ConsistOf(HaveField("Name", "db-2/mongod")))
			})

			It("exits 2 with kind invalid_name and a suggestion when invalid", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Primary stepped down", "--source", "DB-2 Mongod", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_name"`))
				Expect(errOut.String()).To(ContainSubstring("db-2-mongod"))
			})
		})

		Describe("its tags", func() {
			It("keeps a repeated tag once, in the order first given", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Failover", "--tag", "phase:detect", "--tag", "db", "--tag", "phase:detect", "--json")).
					To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"tags":["phase:detect","db"]`))
			})

			It("exits 2 with kind invalid_name and a suggestion when invalid", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Failover", "--tag", "Phase:Detect", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"invalid_name"`))
				Expect(errOut.String()).To(ContainSubstring("phase:detect"))
			})
		})

		DescribeTable("its author comes from --author, then the config file, then USER",
			func(ctx SpecContext, flag []string, config, user, want string) {
				configFile, env["USER"] = config, user

				Expect(run(ctx, append([]string{"event", "add", "Failover", "--json"}, flag...)...)).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"author":` + want))
			},
			Entry("--author first", []string{"--author", "dana"}, `author = "sam"`, "julian", `"dana"`),
			Entry("then the config file", nil, `author = "sam"`, "julian", `"sam"`),
			Entry("then USER", nil, "", "julian", `"julian"`),
			Entry("else none", nil, "", "", `null`),
		)

		DescribeTable("exits 2 on a blank flag value",
			func(ctx SpecContext, flag string) {
				Expect(run(ctx, "event", "add", "Failover", flag, " ")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(flag + " must not be blank"))
			},
			Entry(nil, "--url"),
			Entry(nil, "--author"),
		)
	})

	It("exits 2 with kind timeline_required when no timeline is selected", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0))

		Expect(run(ctx, "event", "add", "Primary stepped down", "--json")).To(Equal(2))
		Expect(errOut.String()).To(ContainSubstring(`"kind":"timeline_required"`))
	})

	It("exits 2 with examples and a pointer to --help when given no text, creating no database", func(ctx SpecContext) {
		Expect(run(ctx, "event", "add")).To(Equal(2))

		Expect(errOut.String()).To(ContainSubstring("$ coroner event add"))
		Expect(errOut.String()).To(ContainSubstring("coroner event add --help"))
		Expect(filepath.Join(home, "coroner.db")).NotTo(BeAnExistingFile())
	})
})
