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

var _ = Describe("annotation", func() {
	const (
		outageID   = "0199a1b2-0000-7000-8000-000000000001"
		stepDownID = "0199a1b2-0000-7000-8000-000000000002"
	)

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

	storedAnnotations := func(ctx SpecContext, timelineID string) []timeline.Annotation {
		GinkgoHelper()
		s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		annotations, err := s.Annotations(ctx, timelineID)
		Expect(err).NotTo(HaveOccurred())
		return annotations
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		now = time.Date(2026, time.September, 13, 1, 30, 5, 250_000_000, time.UTC)
		ids = 0
		in = strings.NewReader("")
		inIsTTY = false
		env = map[string]string{"HOME": home, "USER": "julian"}
		configFile = ""

		Expect(run(ctx, "timeline", "create", "mongo-outage")).To(Equal(0))
		Expect(run(ctx, "timeline", "use", "mongo-outage")).To(Equal(0))
		Expect(run(ctx, "event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z")).To(Equal(0))
	})

	Describe("add", func() {
		It("adds an annotation on an event and prints it as JSON, every key present", func(ctx SpecContext) {
			code := run(ctx, "annotation", "add", "mongod: replSetStepDown received", "-e", "1", "--kind", "evidence",
				"--source", "mongod", "--url", "https://logs.example.com/l/1", "--json")

			Expect(code).To(Equal(0))
			Expect(out.String()).To(MatchJSON(`{
				"id": "0199a1b2-0000-7000-8000-000000000003",
				"timeline_id": "` + outageID + `",
				"event_id": "` + stepDownID + `",
				"kind": "evidence",
				"body": "mongod: replSetStepDown received",
				"author": "julian",
				"source": "mongod",
				"url": "https://logs.example.com/l/1",
				"created_at": "2026-09-13T01:30:05.250000000Z",
				"updated_at": "2026-09-13T01:30:05.250000000Z"
			}`))
			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("EventID", HaveValue(Equal(stepDownID)))))
		})

		It("adds an annotation on the whole timeline when no event is given", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "Step-down lands 5 s after the COLLSCAN burst", "--kind", "comment",
				"--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"event_id":null`))
			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("EventID", BeNil())))
		})

		It("says on stderr what it added and where", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "Election timeout", "-e", "1", "--kind", "comment")).To(Equal(0))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(Equal(
				"Added comment annotation 0199a1b2-0000-7000-8000-000000000003 to event #1 in timeline `mongo-outage`\n"))
		})

		It("prints nothing on stderr under --quiet", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "Election timeout", "--kind", "comment", "-q")).To(Equal(0))

			Expect(errOut.String()).To(BeEmpty())
		})

		It("takes its author from the config file, then USER", func(ctx SpecContext) {
			configFile = `author = "sam"`

			Expect(run(ctx, "annotation", "add", "Election timeout", "--kind", "comment", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"author":"sam"`))
		})

		It("adds to the timeline -t names", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0))

			Expect(run(ctx, "annotation", "add", "Failover drill", "--kind", "comment", "-t", "mongo-failover",
				"--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"timeline_id":"0199a1b2-0000-7000-8000-000000000003"`))
			Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
		})

		Describe("its body", func() {
			It("comes from stdin given -, verbatim but for one trailing newline", func(ctx SpecContext) {
				in = strings.NewReader("[conn42] stepping down\r\n\tretrying  \n\n")

				Expect(run(ctx, "annotation", "add", "-", "--kind", "evidence", "--json")).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"body":"[conn42] stepping down\r\n\tretrying  \n"`))
			})

			It("is never read from stdin unless given -", func(ctx SpecContext) {
				// An open stdin that never closes, as agent harnesses attach.
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in = reader

				Expect(run(ctx, "annotation", "add", "Election timeout", "--kind", "comment")).To(Equal(0))
			})

			DescribeTable("exits 2 with kind usage, adding nothing, when it is missing or blank",
				func(ctx SpecContext, stdin string, stdinIsTTY bool, args ...string) {
					in = strings.NewReader(stdin)
					inIsTTY = stdinIsTTY

					Expect(run(ctx, append([]string{"annotation", "add", "--kind", "evidence", "--json"}, args...)...)).
						To(Equal(2))

					Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
					Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
				},
				Entry("no body", "", false),
				Entry("a blank body", "", false, " \t"),
				Entry("an empty stdin", "", false, "-"),
				Entry("stdin that is a terminal", "", true, "-"),
				Entry("stdin that is not UTF-8", "\xff\xfe", false, "-"),
			)
		})

		DescribeTable("exits 2 with kind usage, naming both kinds, on a kind it does not know or none",
			func(ctx SpecContext, args ...string) {
				Expect(run(ctx, append([]string{"annotation", "add", "Election timeout", "--json"}, args...)...)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
				Expect(errOut.String()).To(ContainSubstring("evidence"))
				Expect(errOut.String()).To(ContainSubstring("comment"))
				Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
			},
			Entry("an unknown kind", "--kind", "note"),
			Entry("no kind"),
		)

		Describe("its event", func() {
			It("exits 3 with kind event_not_found, adding nothing, on an event in another timeline", func(ctx SpecContext) {
				Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0))
				Expect(run(ctx, "event", "add", "Failover drill", "-t", "mongo-failover", "--json")).To(Equal(0))

				for _, ref := range []string{"0199a1b2-0000-7000-8000-000000000004", "mongo-failover#1"} {
					Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-e", ref, "--json")).To(Equal(3))
					Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
				}
				Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
				Expect(storedAnnotations(ctx, "0199a1b2-0000-7000-8000-000000000003")).To(BeEmpty())
			})

			It("exits 3 with kind event_not_found on a seq the timeline does not have", func(ctx SpecContext) {
				Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-e", "9", "--json")).To(Equal(3))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
			})

			It("resolves a unique id prefix", func(ctx SpecContext) {
				Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-e", stepDownID[:30], "--json")).
					To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"event_id":"` + stepDownID + `"`))
			})

			It("exits 2 with kind ambiguous_id, naming the events, on a prefix several share", func(ctx SpecContext) {
				Expect(run(ctx, "event", "add", "Pager fires for db-2")).To(Equal(0))

				Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-e", "0199a1b2", "--json")).
					To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"ambiguous_id"`))
				Expect(errOut.String()).To(ContainSubstring("mongo-outage#1  " + stepDownID))
				Expect(errOut.String()).To(ContainSubstring("mongo-outage#2  0199a1b2-0000-7000-8000-000000000003"))
			})

			It("exits 2 with kind usage on a ref that is neither a seq nor an id", func(ctx SpecContext) {
				Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-e", "Primary", "--json")).
					To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			})
		})

		Describe("--dry-run", func() {
			It("prints the annotation as it would be added, and adds nothing", func(ctx SpecContext) {
				Expect(run(ctx, "annotation", "add", "Election timeout", "-e", "1", "--kind", "comment", "--dry-run",
					"--json")).To(Equal(0))

				Expect(out.String()).To(ContainSubstring(`"event_id":"` + stepDownID + `","kind":"comment"`))
				Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
			})

			It("describes the annotation in text", func(ctx SpecContext) {
				Expect(run(ctx, "annotation", "add", "Election timeout", "-e", "1", "--kind", "comment", "--dry-run")).
					To(Equal(0))

				Expect(out.String()).To(HavePrefix("Would add comment annotation to event #1 in timeline `mongo-outage`\n"))
				Expect(out.String()).To(MatchRegexp(`(?m)^Body: +Election timeout$`))
				Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
			})
		})
	})
})
