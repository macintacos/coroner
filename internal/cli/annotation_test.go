package cli_test

import (
	"bytes"
	"encoding/json"
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
				"updated_at": "2026-09-13T01:30:05.250000000Z",
				"attachments": []
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

		DescribeTable("exits 2 with kind usage on a flag that is not UTF-8, adding nothing",
			func(ctx SpecContext, flag, value string) {
				Expect(run(ctx, "annotation", "add", "election log", "-e", "1", "--kind", "evidence", "--json", flag, value)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
				Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
				Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
			},
			Entry(nil, "--url", "https://logs.example.com/\xff"),
			Entry(nil, "--author", "julian\xff"),
		)

		It("exits 2 with kind usage on a USER author that is not UTF-8, adding nothing", func(ctx SpecContext) {
			env["USER"] = "julian\xff"

			Expect(run(ctx, "annotation", "add", "election log", "-e", "1", "--kind", "evidence", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
			Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
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

			It("resolves an id or timeline#seq in the timeline -t names", func(ctx SpecContext) {
				for _, ref := range []string{stepDownID, "mongo-outage#1"} {
					Expect(run(ctx, "annotation", "add", "Drill log", "--kind", "evidence", "-t", "mongo-outage", "-e", ref)).
						To(Equal(0))
				}

				Expect(storedAnnotations(ctx, outageID)).To(HaveLen(2))
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

			It("exits 2 with kind usage, before reading stdin, on a ref that is neither a seq nor an id", func(ctx SpecContext) {
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in = reader

				Expect(run(ctx, "annotation", "add", "-", "--kind", "evidence", "-e", "Primary", "--json")).To(Equal(2))

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

	Describe("list", func() {
		listed := func() []map[string]any {
			GinkgoHelper()
			var body struct {
				Annotations []map[string]any `json:"annotations"`
			}
			Expect(json.Unmarshal(out.Bytes(), &body)).To(Succeed())
			return body.Annotations
		}
		listedIDs := func() []any {
			GinkgoHelper()
			ids := []any{}
			for _, a := range listed() {
				ids = append(ids, a["id"])
			}
			return ids
		}
		addAnnotation := func(ctx SpecContext, args ...string) {
			GinkgoHelper()
			now = now.Add(time.Minute)
			Expect(run(ctx, append([]string{"annotation", "add"}, args...)...)).To(Equal(0))
		}

		It("lists the timeline's own annotations first, then by when their event happened, then oldest first",
			func(ctx SpecContext) {
				// #2 happened before #1.
				Expect(run(ctx, "event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:10:00Z")).To(Equal(0))
				addAnnotation(ctx, "election log", "-e", "1", "--kind", "evidence")
				addAnnotation(ctx, "pager log", "-e", "2", "--kind", "evidence")
				addAnnotation(ctx, "Root cause: undersized oplog", "--kind", "comment")
				addAnnotation(ctx, "Pager fired late", "-e", "2", "--kind", "comment")

				Expect(run(ctx, "annotation", "list", "--json")).To(Equal(0))

				Expect(listedIDs()).To(Equal([]any{
					"0199a1b2-0000-7000-8000-000000000006",
					"0199a1b2-0000-7000-8000-000000000005",
					"0199a1b2-0000-7000-8000-000000000007",
					"0199a1b2-0000-7000-8000-000000000004",
				}))
			})

		It("returns a body read from stdin byte for byte, less one trailing newline", func(ctx SpecContext) {
			log := "2026-09-11T03:14:07.412+0000 I REPL [conn42] stepping down\r\n\t└ reason: \"election\"  \n\n"
			in = strings.NewReader(log)
			addAnnotation(ctx, "-", "-e", "1", "--kind", "evidence")

			Expect(run(ctx, "annotation", "list", "--json")).To(Equal(0))

			Expect(listed()).To(ConsistOf(HaveKeyWithValue("body", strings.TrimSuffix(log, "\n"))))
		})

		DescribeTable("lists only the annotations on the event -e names",
			func(ctx SpecContext, ref string) {
				Expect(run(ctx, "event", "add", "Pager fires for db-2")).To(Equal(0))
				addAnnotation(ctx, "election log", "-e", "1", "--kind", "evidence")
				addAnnotation(ctx, "pager log", "-e", "2", "--kind", "evidence")
				addAnnotation(ctx, "Root cause: undersized oplog", "--kind", "comment")

				Expect(run(ctx, "annotation", "list", "-e", ref, "--json")).To(Equal(0))

				Expect(listedIDs()).To(Equal([]any{"0199a1b2-0000-7000-8000-000000000005"}))
			},
			Entry("by seq", "2"),
			Entry("by timeline and seq", "mongo-outage#2"),
			Entry("by id", "0199a1b2-0000-7000-8000-000000000003"),
		)

		It("exits 3 with kind event_not_found on an event in another timeline", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0))
			Expect(run(ctx, "event", "add", "Failover drill", "-t", "mongo-failover")).To(Equal(0))

			Expect(run(ctx, "annotation", "list", "-e", "mongo-failover#1", "--json")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
		})

		It("lists the timeline -t names", func(ctx SpecContext) {
			addAnnotation(ctx, "Root cause: undersized oplog", "--kind", "comment")
			Expect(run(ctx, "timeline", "create", "mongo-failover")).To(Equal(0))

			Expect(run(ctx, "annotation", "list", "-t", "mongo-failover", "--json")).To(Equal(0))

			Expect(out.String()).To(Equal("{\"annotations\":[]}\n"))
		})

		It("prints a tab-separated row per annotation when piped: id, kind, author, event and first line", func(ctx SpecContext) {
			addAnnotation(ctx, "Root cause: undersized oplog\nSee the capacity review.", "--kind", "comment")
			addAnnotation(ctx, "election log", "-e", "1", "--kind", "evidence", "--author", "sam")

			Expect(run(ctx, "annotation", "list")).To(Equal(0))

			Expect(out.String()).To(Equal(
				"0199a1b2-0000-7000-8000-000000000003\tcomment\tjulian\t-\tRoot cause: undersized oplog\n" +
					"0199a1b2-0000-7000-8000-000000000004\tevidence\tsam\t#1\telection log\n"))
		})

		It("says on stderr when there are none, and prints nothing", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "list")).To(Equal(0))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring("coroner annotation add"))
		})
	})

	Describe("edit", func() {
		const electionLogID = "0199a1b2-0000-7000-8000-000000000003"
		created := "2026-09-13T01:30:05.250000000Z"

		BeforeEach(func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "election log", "-e", "1", "--kind", "evidence", "--source", "mongod",
				"--url", "https://logs.example.com/l/1")).To(Equal(0))
			now = now.Add(time.Hour)
		})

		It("changes the fields given, keeping its author, event and created at, and prints it as JSON", func(ctx SpecContext) {
			env["USER"] = "sam"

			Expect(run(ctx, "annotation", "edit", electionLogID, "--kind", "comment", "--body",
				"Election timeout, not a partition", "--source", "slack", "--url", "https://chat.example.com/p/9",
				"--json")).To(Equal(0))

			Expect(out.String()).To(MatchJSON(`{
				"id": "` + electionLogID + `",
				"timeline_id": "` + outageID + `",
				"event_id": "` + stepDownID + `",
				"kind": "comment",
				"body": "Election timeout, not a partition",
				"author": "julian",
				"source": "slack",
				"url": "https://chat.example.com/p/9",
				"created_at": "` + created + `",
				"updated_at": "2026-09-13T02:30:05.250000000Z",
				"attachments": []
			}`))
			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(SatisfyAll(
				HaveField("Kind", timeline.AnnotationComment), HaveField("Source", HaveValue(Equal("slack"))))))
		})

		It("leaves the fields not given as they are", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", electionLogID, "--kind", "comment", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"body":"election log","author":"julian","source":"mongod"`))
		})

		It("exits 2 with kind usage on a --url that is not UTF-8", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", electionLogID, "--url", "https://logs.example.com/\xff", "--json")).
				To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
			Expect(run(ctx, "timeline", "export", "--format", "jsonl")).To(Equal(0), errOut.String())
		})

		It("reads --body - from stdin", func(ctx SpecContext) {
			in = strings.NewReader("[conn42] stepping down\n")

			Expect(run(ctx, "annotation", "edit", electionLogID, "--body", "-", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"body":"[conn42] stepping down"`))
		})

		It("says on stderr what it edited", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", electionLogID, "--kind", "comment")).To(Equal(0))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(Equal("Edited comment annotation " + electionLogID + " in timeline `mongo-outage`\n"))
		})

		It("prints the edited annotation with --dry-run, and changes nothing", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", electionLogID, "--kind", "comment", "--dry-run", "--json")).To(Equal(0))

			Expect(out.String()).To(ContainSubstring(`"kind":"comment"`))
			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("Kind", timeline.AnnotationEvidence)))
		})

		It("describes the edited annotation in text with --dry-run, kind included", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", electionLogID, "--kind", "comment", "--dry-run")).To(Equal(0))

			Expect(out.String()).To(HavePrefix("Would edit annotation " + electionLogID + " to\n"))
			Expect(out.String()).To(MatchRegexp(`(?m)^Kind: +comment$`))
		})

		It("finds the annotation by an upper-case id", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", strings.ToUpper(electionLogID), "--kind", "comment")).To(Equal(0))

			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("Kind", timeline.AnnotationComment)))
		})

		It("finds the annotation by a unique id prefix, with no timeline selected", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0))

			Expect(run(ctx, "annotation", "edit", electionLogID[:30], "--kind", "comment")).To(Equal(0))

			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("Kind", timeline.AnnotationComment)))
		})

		It("exits 3 with kind annotation_not_found on an id no annotation has", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "edit", "0199a1b2-ffff", "--kind", "comment", "--json")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"annotation_not_found"`))
		})

		It("exits 2 with kind ambiguous_id, naming the annotations, on a prefix several share", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "Root cause: undersized oplog", "--kind", "comment")).To(Equal(0))

			Expect(run(ctx, "annotation", "edit", "0199a1b2", "--kind", "comment", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"ambiguous_id"`))
			Expect(errOut.String()).To(ContainSubstring(electionLogID))
			Expect(errOut.String()).To(ContainSubstring("0199a1b2-0000-7000-8000-000000000005"))
		})

		DescribeTable("exits 2, changing nothing and never waiting on stdin, on a command line it cannot apply",
			func(ctx SpecContext, kind string, args ...string) {
				reader, writer := io.Pipe()
				DeferCleanup(writer.Close)
				in = reader

				Expect(run(ctx, append([]string{"annotation", "edit", "--json"}, args...)...)).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"` + kind + `"`))
				Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(SatisfyAll(
					HaveField("Kind", timeline.AnnotationEvidence), HaveField("Body", "election log"))))
			},
			Entry("no field to edit", "usage", electionLogID),
			Entry("an unknown kind", "usage", electionLogID, "--kind", "note"),
			Entry("a blank body", "usage", electionLogID, "--body", " "),
			Entry("a blank URL", "usage", electionLogID, "--url", ""),
			Entry("a source outside the grammar", "invalid_name", electionLogID, "--source", "DB 2"),
			Entry("a ref that is not an id", "usage", "Election", "--body", "-"),
		)
	})

	Describe("delete", func() {
		const (
			electionLogID = "0199a1b2-0000-7000-8000-000000000003"
			rootCauseID   = "0199a1b2-0000-7000-8000-000000000004"
		)

		BeforeEach(func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "add", "election log", "-e", "1", "--kind", "evidence")).To(Equal(0))
			Expect(run(ctx, "annotation", "add", "Root cause: undersized oplog", "--kind", "comment")).To(Equal(0))
		})

		It("deletes every annotation named and says so on stderr", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", electionLogID, rootCauseID)).To(Equal(0))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(Equal(
				"Deleted evidence annotation " + electionLogID + " from timeline `mongo-outage`\n" +
					"Deleted comment annotation " + rootCauseID + " from timeline `mongo-outage`\n"))
			Expect(storedAnnotations(ctx, outageID)).To(BeEmpty())
		})

		It("prints the deleted annotations as JSON", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", rootCauseID, electionLogID, "--json")).To(Equal(0))

			Expect(out.String()).To(HavePrefix(`{"annotations":[{"id":"` + rootCauseID + `",`))
			Expect(out.String()).To(ContainSubstring(`{"id":"` + electionLogID + `",`))
		})

		It("deletes an annotation named twice once", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", electionLogID, electionLogID)).To(Equal(0))

			Expect(strings.Count(errOut.String(), "Deleted")).To(Equal(1))
			Expect(storedAnnotations(ctx, outageID)).To(ConsistOf(HaveField("ID", rootCauseID)))
		})

		It("exits 3 with kind annotation_not_found, deleting none, when one id matches nothing", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", electionLogID, "0199a1b2-ffff", "--json")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"annotation_not_found"`))
			Expect(storedAnnotations(ctx, outageID)).To(HaveLen(2))
		})

		It("says on stdout what --dry-run would delete, and deletes nothing", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", electionLogID, rootCauseID, "--dry-run")).To(Equal(0))

			Expect(out.String()).To(Equal(
				"Would delete evidence annotation " + electionLogID + " from timeline `mongo-outage`\n" +
					"Would delete comment annotation " + rootCauseID + " from timeline `mongo-outage`\n"))
			Expect(storedAnnotations(ctx, outageID)).To(HaveLen(2))
		})

		It("exits 2 with kind usage when no id is given", func(ctx SpecContext) {
			Expect(run(ctx, "annotation", "delete", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
		})
	})
})
