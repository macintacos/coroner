package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rogpeppe/go-internal/diff"
	"github.com/rogpeppe/go-internal/txtar"
	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
func TestExportImportRoundTrip(t *testing.T) {
	runs := t.TempDir()
	seen := map[string]bool{}

	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp(runs, "run")
		if err != nil {
			t.Fatal(err)
		}
		err = checkRoundTrip(t.Context(), dir, roundTripModel().Draw(t, "model"), seen)
		if err != nil {
			// rapid shrinks a failure only while its message repeats, and every run has its own directory.
			t.Fatal(strings.ReplaceAll(err.Error(), dir, "$RUN"))
		}
	})

	// Fewer checks than rapid's default, as when debugging, draw too few models to cover everything.
	checks, _ := strconv.Atoi(flag.Lookup("rapid.checks").Value.String())
	if missing := unseen(seen); checks >= 100 && len(missing) > 0 {
		t.Errorf("no generated export held %s", strings.Join(missing, ", "))
	}
}

var _ = Describe("Frozen export fixtures", func() {
	paths, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "export", "v*", "*"))
	var entries []TableEntry
	for _, path := range paths {
		if !strings.HasPrefix(filepath.Base(path), ".") {
			entries = append(entries, Entry(filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)), path))
		}
	}

	DescribeTable("round-trip through import into an empty store, exporting the current version's fixture of that name",
		func(ctx SpecContext, path string) {
			dir := GinkgoT().TempDir()
			records, importable := importableFixture(dir, path)
			writeSidecars(dir)
			db := filepath.Join(dir, "coroner.db")
			_, err := coroner{home: dir}.run(ctx, "--db", db, "import", importable)
			Expect(err).NotTo(HaveOccurred())

			want, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), "v"+export.FormatVersion, filepath.Base(path)))
			Expect(err).NotTo(HaveOccurred())
			Expect(reexport(ctx, db, strings.TrimPrefix(filepath.Ext(path), "."), records)).To(Equal(string(want)))
		},
		entries,
	)
})

// checkRoundTrip writes m into dir/a.db and round-trips it, recording in seen what its export
// held.
func checkRoundTrip(ctx context.Context, dir string, m model, seen map[string]bool) error {
	err := seedStore(ctx, filepath.Join(dir, "a.db"), m)
	if err != nil {
		return err
	}
	first, err := roundTrip(ctx, dir, m.names())
	if err != nil {
		return err
	}

	return observe(seen, first)
}

// importableFixture returns the records of the fixture at path, and a JSONL file in dir that import
// reads them from: a link to the fixture itself, or a JSON document's records.
func importableFixture(dir, path string) ([]export.Record, string) {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())
	importable := filepath.Join(dir, "fixture.jsonl")

	if filepath.Ext(path) == ".jsonl" {
		records, err := export.Decode(bytes.NewReader(data))
		Expect(err).NotTo(HaveOccurred())
		fixture, err := filepath.Abs(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Symlink(fixture, importable)).To(Succeed())

		return records, importable
	}

	var env export.Envelope
	Expect(json.Unmarshal(data, &env)).To(Succeed())
	records, err := export.Records(env)
	Expect(err).NotTo(HaveOccurred())
	var jsonl bytes.Buffer
	Expect(export.WriteRecords(&jsonl, records)).To(Succeed())
	Expect(os.WriteFile(importable, jsonl.Bytes(), 0o600)).To(Succeed())

	return records, importable
}

// writeSidecars writes each file export-fixtures.txtar archives, which the fixtures' attachments
// were made from, into dir/attachments/ under its sha256, as an export written with -o <dir>/
// holds them.
func writeSidecars(dir string) {
	GinkgoHelper()
	archive, err := txtar.ParseFile("testdata/script/contract/export-fixtures.txtar")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.MkdirAll(filepath.Join(dir, "attachments"), 0o750)).To(Succeed())

	for _, f := range archive.Files {
		sum := sha256.Sum256(f.Data)
		Expect(os.WriteFile(filepath.Join(dir, "attachments", hex.EncodeToString(sum[:])), f.Data, 0o600)).To(Succeed())
	}
}

// reexport returns what exporting db as format writes again for each header in records, in
// order: the header's timelines, at its exported at, by its coroner version.
func reexport(ctx context.Context, db, format string, records []export.Record) string {
	GinkgoHelper()
	var out strings.Builder
	for _, record := range records {
		header, ok := record.(export.Header)
		if !ok {
			continue
		}
		filters := header.Query
		filters.Timelines = nil
		Expect(filters).To(Equal(export.Query{Sources: []string{}, Tags: []string{}, Order: "asc"}), "reexport passes on a query's timelines only")

		args := []string{"--db", db, "timeline", "export", "--format", format}
		for _, name := range header.Query.Timelines {
			args = append(args, "-t", name)
		}
		c := coroner{home: filepath.Dir(db), now: time.Time(header.ExportedAt), version: header.CoronerVersion}
		stdout, err := c.run(ctx, args...)
		Expect(err).NotTo(HaveOccurred())
		out.WriteString(stdout)
	}

	return out.String()
}

// model is what a round trip writes through the store API, in the order it writes it.
type model struct {
	timelines   []timeline.Timeline
	events      []eventDraft
	annotations []annotationDraft
}

type eventDraft struct {
	timeline.Event

	newSourceID string
	files       []file
}

type annotationDraft struct {
	timeline.Annotation

	newSourceID string
	files       []file
}

type file struct {
	id, name string
	created  time.Time
	content  []byte
}

func (m model) names() []string {
	names := make([]string, 0, len(m.timelines))
	for _, t := range m.timelines {
		names = append(names, t.Name)
	}

	return names
}

// seedStore writes m into a new database at path through the store API.
func seedStore(ctx context.Context, path string, m model) error {
	s, err := store.Open(ctx, path)
	if err != nil {
		return err
	}

	return errors.Join(writeModel(ctx, s, m), s.Close())
}

func writeModel(ctx context.Context, s *store.Store, m model) error {
	for _, t := range m.timelines {
		err := s.CreateTimeline(ctx, t)
		if err != nil {
			return err
		}
	}
	for _, e := range m.events {
		_, err := s.AddEvent(ctx, e.Event, e.newSourceID, newAttachments(e.files)...)
		if err != nil {
			return err
		}
	}
	for _, a := range m.annotations {
		err := s.AddAnnotation(ctx, a.Annotation, a.newSourceID, newAttachments(a.files)...)
		if err != nil {
			return err
		}
	}

	return nil
}

func newAttachments(files []file) []store.NewAttachment {
	attachments := make([]store.NewAttachment, 0, len(files))
	for _, f := range files {
		attachments = append(attachments, store.NewAttachment{ID: f.id, Filename: f.name, CreatedAt: f.created, Content: bytes.NewReader(f.content)})
	}

	return attachments
}

// roundTrip exports the timelines names from dir/a.db into dir/a/, imports that export into an
// empty dir/b.db, and exports b.db into dir/b/. It returns the first export, and an error unless
// both directories hold the same bytes.
func roundTrip(ctx context.Context, dir string, names []string) ([]byte, error) {
	c := coroner{home: dir, now: time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC), version: "(devel)"}
	jsonl := strings.Join(names, "+") + ".jsonl"

	_, err := c.run(ctx, exportArgs(filepath.Join(dir, "a.db"), names, filepath.Join(dir, "a")+string(filepath.Separator))...)
	if err != nil {
		return nil, err
	}
	_, err = c.run(ctx, "--db", filepath.Join(dir, "b.db"), "import", filepath.Join(dir, "a", jsonl))
	if err != nil {
		return nil, err
	}
	_, err = c.run(ctx, exportArgs(filepath.Join(dir, "b.db"), names, filepath.Join(dir, "b")+string(filepath.Separator))...)
	if err != nil {
		return nil, err
	}

	differences, err := dirDiff(filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if err != nil || differences != "" {
		return nil, errors.Join(err, fmt.Errorf("the export changed on its way through import:\n%s", differences))
	}

	return os.ReadFile(filepath.Clean(filepath.Join(dir, "a", jsonl)))
}

func exportArgs(db string, names []string, into string) []string {
	args := []string{"--db", db, "timeline", "export", "--format", "jsonl", "-o", into}
	for _, name := range names {
		args = append(args, "-t", name)
	}

	return args
}

// coroner runs commands as the binary would, against real files, with a fixed clock and
// version and IDs numbered from 1.
type coroner struct {
	home    string
	now     time.Time
	version string
}

// run returns what the command line args wrote to stdout, or an error holding its stderr unless
// it exited 0.
func (c coroner) run(ctx context.Context, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	var ids uint64
	code := cli.Run(ctx, cli.App{
		Args:       args,
		In:         strings.NewReader(""),
		Out:        &out,
		Err:        &errOut,
		Version:    c.version,
		LookupEnv:  func(key string) (string, bool) { return map[string]string{"HOME": c.home}[key], key == "HOME" },
		ReadFile:   os.ReadFile,
		WriteFile:  os.WriteFile,
		OpenFile:   openFile,
		RemoveFile: os.Remove,
		ReadDir:    os.ReadDir,
		Stat:       os.Stat,
		MkdirAll:   os.MkdirAll,
		Now:        func() time.Time { return c.now },
		NewID: func() (uuid.UUID, error) {
			ids++
			return testID(c.now.UnixMilli(), ids), nil
		},
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	})
	if code != 0 {
		return "", fmt.Errorf("coroner %s exited %d:\n%s", strings.Join(args, " "), code, errOut.String())
	}

	return out.String(), nil
}

// dirDiff returns a diff of every file that is in only one of the directories a and b, or differs
// between them, and "" when they hold the same files.
func dirDiff(a, b string) (string, error) {
	filesA, errA := readTree(a)
	filesB, errB := readTree(b)
	if errA != nil || errB != nil {
		return "", errors.Join(errA, errB)
	}

	either := maps.Clone(filesA)
	maps.Copy(either, filesB)
	var out strings.Builder
	for _, name := range slices.Sorted(maps.Keys(either)) {
		dataA, inA := filesA[name]
		dataB, inB := filesB[name]
		if inA != inB {
			fmt.Fprintf(&out, "%s is only in %s\n", name, map[bool]string{true: a, false: b}[inA])
		}
		if !bytes.Equal(dataA, dataB) {
			out.Write(diff.Diff(filepath.Join(a, name), dataA, filepath.Join(b, name), dataB))
		}
	}

	return out.String(), nil
}

// readTree returns the content of every file under dir, by its path relative to dir.
func readTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[path], err = fs.ReadFile(os.DirFS(dir), path)
		return err
	})

	return files, err
}

// observe records in seen each record type in jsonl, and each of its keys as type.key=null or
// type.key=set.
func observe(seen map[string]bool, jsonl []byte) error {
	for line := range bytes.Lines(jsonl) {
		var record map[string]any
		err := json.Unmarshal(line, &record)
		if err != nil {
			return err
		}
		typ, _ := record["type"].(string)
		seen[typ] = true
		for key, value := range record {
			state := "set"
			if value == nil {
				state = "null"
			}
			seen[typ+"."+key+"="+state] = true
		}
	}

	return nil
}

// unseen returns what seen lacks of every record type and, for each pointer field of its export
// type, its key both null and set. An exported event always has a seq.
func unseen(seen map[string]bool) []string {
	var missing []string
	for typ, record := range map[string]any{
		"timeline": export.Timeline{}, "source": export.Source{}, "event": export.Event{},
		"annotation": export.Annotation{}, "attachment": export.Attachment{},
	} {
		want := []string{typ}
		for field := range reflect.TypeOf(record).Fields() {
			key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.Type.Kind() == reflect.Pointer && typ+"."+key != "event.seq" {
				want = append(want, typ+"."+key+"=null", typ+"."+key+"=set")
			}
		}
		for _, w := range want {
			if !seen[w] {
				missing = append(missing, w)
			}
		}
	}
	slices.Sort(missing)

	return missing
}

// roundTripModel draws 1–3 timelines with events, annotations and attachments that every
// validation accepts, so a failure is a round trip losing something rather than a rejected
// record. Instants often tie, so seq decides the order.
func roundTripModel() *rapid.Generator[model] {
	return rapid.Custom(func(t *rapid.T) model {
		n := 0
		nextID := func() string {
			n++
			return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
		}
		sources := rapid.SliceOfNDistinct(sourceName(), 0, 3, rapid.ID).Draw(t, "sources")

		var m model
		for _, name := range rapid.SliceOfNDistinct(timelineName(), 1, 3, rapid.ID).Draw(t, "names") {
			tl := timeline.Timeline{
				ID: nextID(), Name: name, Title: rapid.OneOf(rapid.Just(""), text()).Draw(t, "title"),
				Refs: rapid.SliceOfNDistinct(text(), 0, 3, rapid.ID).Draw(t, "refs"), CreatedAt: instant().Draw(t, "created"),
			}
			tl.UpdatedAt = tl.CreatedAt
			m.timelines = append(m.timelines, tl)

			var eventIDs []string
			for range rapid.IntRange(0, 6).Draw(t, "events") {
				e := event(sources).Draw(t, "event")
				e.ID, e.TimelineID = nextID(), tl.ID
				eventIDs = append(eventIDs, e.ID)
				m.events = append(m.events, eventDraft{Event: e, newSourceID: nextID(), files: files(nextID).Draw(t, "files")})
			}
			for range rapid.IntRange(0, 4).Draw(t, "annotations") {
				a := annotation(sources, eventIDs).Draw(t, "annotation")
				a.ID, a.TimelineID = nextID(), tl.ID
				m.annotations = append(m.annotations, annotationDraft{Annotation: a, newSourceID: nextID(), files: files(nextID).Draw(t, "files")})
			}
		}

		return m
	})
}

// event draws an event without ids, its occurred at the start of its precision window in its
// offset.
func event(sources []string) *rapid.Generator[timeline.Event] {
	return rapid.Custom(func(t *rapid.T) timeline.Event {
		e := timeline.Event{
			Precision: rapid.SampledFrom([]timeline.Precision{
				timeline.PrecisionDay, timeline.PrecisionHour, timeline.PrecisionMinute, timeline.PrecisionSecond,
				timeline.PrecisionMillisecond, timeline.PrecisionMicrosecond, timeline.PrecisionNanosecond,
			}).Draw(t, "precision"),
			OccurredOffset: rapid.Ptr(rapid.IntRange(-86400, 86400), true).Draw(t, "offset"),
			Approximate:    rapid.Bool().Draw(t, "approximate"),
			OccurredRaw:    rapid.Ptr(text(), true).Draw(t, "raw"),
			Source:         optionalSource(sources).Draw(t, "source"),
			Tags:           rapid.SliceOfNDistinct(tag(), 0, 4, rapid.ID).Draw(t, "tags"),
			Text:           text().Draw(t, "text"),
			URL:            rapid.Ptr(text(), true).Draw(t, "url"),
			Author:         rapid.Ptr(text(), true).Draw(t, "author"),
			CreatedAt:      instant().Draw(t, "created"),
			UpdatedAt:      instant().Draw(t, "updated"),
		}
		zone := time.UTC
		if e.OccurredOffset != nil {
			zone = time.FixedZone("", *e.OccurredOffset)
		}
		e.OccurredAt = e.Precision.Truncate(instant().Draw(t, "occurred").In(zone)).UTC()
		if rapid.Bool().Draw(t, "interval") {
			e.EndedAt = new(e.OccurredAt.Add(time.Duration(rapid.Int64Range(0, int64(48*time.Hour)).Draw(t, "length"))))
		}

		return e
	})
}

// annotation draws an annotation without ids, on one of eventIDs or on its whole timeline.
func annotation(sources, eventIDs []string) *rapid.Generator[timeline.Annotation] {
	return rapid.Custom(func(t *rapid.T) timeline.Annotation {
		a := timeline.Annotation{
			Kind:      rapid.SampledFrom([]timeline.AnnotationKind{timeline.AnnotationEvidence, timeline.AnnotationComment}).Draw(t, "kind"),
			Body:      text().Draw(t, "body"),
			Author:    rapid.Ptr(text(), true).Draw(t, "author"),
			Source:    optionalSource(sources).Draw(t, "source"),
			URL:       rapid.Ptr(text(), true).Draw(t, "url"),
			CreatedAt: instant().Draw(t, "created"),
			UpdatedAt: instant().Draw(t, "updated"),
		}
		if len(eventIDs) > 0 {
			a.EventID = rapid.Ptr(rapid.SampledFrom(eventIDs), true).Draw(t, "event")
		}

		return a
	})
}

// files draws up to two attachment files, whose content often repeats so attachments share a blob.
func files(nextID func() string) *rapid.Generator[[]file] {
	return rapid.Custom(func(t *rapid.T) []file {
		var out []file
		for range rapid.IntRange(0, 2).Draw(t, "count") {
			out = append(out, file{
				id: nextID(), name: text().Draw(t, "filename"), created: instant().Draw(t, "attached"),
				content: rapid.OneOf(rapid.SampledFrom([][]byte{{}, []byte("shared\n")}), rapid.SliceOfN(rapid.Byte(), 0, 64)).Draw(t, "content"),
			})
		}

		return out
	})
}

func optionalSource(sources []string) *rapid.Generator[*string] {
	if len(sources) == 0 {
		return rapid.Just[*string](nil)
	}

	return rapid.Ptr(rapid.SampledFrom(sources), true)
}

func timelineName() *rapid.Generator[string] {
	return rapid.StringMatching(`^[a-z0-9][a-z0-9._-]{0,63}$`)
}

func sourceName() *rapid.Generator[string] {
	return tag().Filter(func(name string) bool { return name != "none" })
}

func tag() *rapid.Generator[string] {
	return rapid.StringMatching(`^[a-z0-9][a-z0-9._:/-]{0,63}$`)
}

// text draws text the store's not-blank CHECK accepts: any Unicode, text that breaks HTML, lines
// or JSON, or a long run.
func text() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringN(1, 32, -1),
		rapid.SampledFrom([]string{"</script><script>alert(1)</script>", "line one\nline two\r\n", "Réplica caída — 東京 ✓", "a\u2028b\u0000c"}),
		rapid.StringN(1000, 5000, -1),
	).Filter(func(s string) bool {
		// SQLite's length() stops at a NUL, so the CHECK reads text whose trimmed start is NUL as blank.
		trimmed := strings.TrimSpace(s)
		return trimmed != "" && trimmed[0] != 0
	})
}

// instant draws an instant, usually in one of four seconds so instants tie, at a random precision
// so fractions land on and beside their boundaries. Years 0001–9998 leave room for an offset and
// an interval.
func instant() *rapid.Generator[time.Time] {
	near := time.Date(2026, time.September, 11, 3, 14, 7, 0, time.UTC).Unix()
	first := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	last := time.Date(9998, time.December, 31, 0, 0, 0, 0, time.UTC).Unix()

	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := rapid.OneOf(rapid.Int64Range(near, near+3), rapid.Int64Range(first, last)).Draw(t, "sec")
		unit := rapid.SampledFrom([]int64{1, 1e3, 1e6, 1e9}).Draw(t, "unit")
		nsec := rapid.Int64Range(0, int64(time.Second)/unit-1).Draw(t, "units") * unit
		if rapid.Bool().Draw(t, "last in its unit") {
			nsec = int64(time.Second) - unit
		}

		return time.Unix(sec, nsec).UTC()
	})
}
