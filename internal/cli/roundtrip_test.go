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
	"io"
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

// This file belongs to the contract suite: weakening what it requires is a contract change.

const exportFixtures = "../../testdata/export"

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
func TestExportImportRoundTrip(t *testing.T) {
	runs := t.TempDir()
	seen := map[string]bool{}

	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp(runs, "run")
		if err != nil {
			t.Fatal(err)
		}
		first, err := checkRoundTrip(t.Context(), dir, roundTripModel().Draw(t, "model"))
		if err == nil {
			err = observe(seen, first)
		}
		if err != nil {
			// rapid shrinks a failure only while its message repeats, and every run has its own directory.
			t.Fatal(strings.ReplaceAll(err.Error(), dir, "$RUN"))
		}
		err = os.RemoveAll(dir)
		if err != nil {
			t.Fatal(err)
		}
	})

	// With fewer checks than rapid's default 100, as when debugging, or under -short, which runs a
	// fifth of them, too few models are drawn to cover every key.
	checks, _ := strconv.Atoi(flag.Lookup("rapid.checks").Value.String())
	if missing := unseen(seen); checks >= 100 && !testing.Short() && len(missing) > 0 {
		t.Errorf("no generated export held %s", strings.Join(missing, ", "))
	}
}

var _ = Describe("Frozen export fixtures", func() {
	names, globErr := fs.Glob(os.DirFS(exportFixtures), "v*/*")
	var entries []TableEntry
	for _, name := range names {
		if !strings.HasPrefix(filepath.Base(name), ".") {
			entries = append(entries, Entry(name, name))
		}
	}

	It("finds the frozen fixtures", func() {
		Expect(globErr).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty())
	})

	DescribeTable("round-trip through import into an empty database, exporting the current version's fixture of that name",
		func(ctx SpecContext, name string) {
			dir := GinkgoT().TempDir()
			f := importableFixture(dir, name)
			writeSidecars(dir)
			_, err := coroner{home: dir}.run(ctx, "--db", filepath.Join(dir, "coroner.db"), "import", f.file)
			Expect(err).NotTo(HaveOccurred())

			want, err := os.ReadFile(currentFixture(name))
			Expect(err).NotTo(HaveOccurred())
			Expect(reexport(ctx, dir, f)).To(Equal(string(want)))
		},
		entries,
	)
})

// checkRoundTrip writes m into dir/a.db and round-trips it, returning its first export.
func checkRoundTrip(ctx context.Context, dir string, m model) ([]byte, error) {
	err := seedStore(ctx, filepath.Join(dir, "a.db"), m)
	if err != nil {
		return nil, err
	}

	return roundTrip(ctx, dir, m.names())
}

// fixture is a frozen fixture as import reads it: its records, in a JSONL file.
type fixture struct {
	format, file string
	records      []export.Record
}

func currentFixture(name string) string {
	return filepath.Join(exportFixtures, "v"+export.FormatVersion, filepath.Base(name))
}

// importableFixture reads the fixture name and writes its records as a JSONL file in dir, beside
// the attachments/ import reads sidecars from.
func importableFixture(dir, name string) fixture {
	GinkgoHelper()
	path := filepath.Clean(filepath.Join(exportFixtures, name))
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	f := fixture{format: strings.TrimPrefix(filepath.Ext(name), "."), file: filepath.Join(dir, "fixture.jsonl")}

	if f.format == "json" {
		var env export.Envelope
		Expect(json.Unmarshal(data, &env)).To(Succeed())
		f.records, err = export.Records(env)
		Expect(err).NotTo(HaveOccurred())
		var jsonl bytes.Buffer
		Expect(export.WriteRecords(&jsonl, f.records)).To(Succeed())
		Expect(os.WriteFile(f.file, jsonl.Bytes(), 0o600)).To(Succeed())

		return f
	}

	f.records, err = export.Decode(bytes.NewReader(data))
	Expect(err).NotTo(HaveOccurred())
	src, err := os.Open(path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(src.Close)
	dst, err := os.Create(f.file)
	Expect(err).NotTo(HaveOccurred())
	_, err = io.Copy(dst, src)
	Expect(err).NotTo(HaveOccurred())
	Expect(dst.Close()).To(Succeed())

	return f
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

// reexport returns what exporting dir/coroner.db as f's format writes for each header in f's
// records, in order: the header's timelines, at its exported_at, as its coroner_version.
func reexport(ctx context.Context, dir string, f fixture) string {
	GinkgoHelper()
	var out strings.Builder
	for _, record := range f.records {
		header, ok := record.(export.Header)
		if !ok {
			continue
		}
		filters := header.Query
		filters.Timelines = nil
		Expect(filters).To(Equal(export.Query{Sources: []string{}, Tags: []string{}, Order: "asc"}), "reexport passes on a query's timelines only")

		args := []string{"--db", filepath.Join(dir, "coroner.db"), "timeline", "export", "--format", f.format}
		for _, name := range header.Query.Timelines {
			args = append(args, "-t", name)
		}
		c := coroner{home: dir, now: time.Time(header.ExportedAt), version: header.CoronerVersion}
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
	attachments []attachmentDraft
	deleted     bool
}

type annotationDraft struct {
	timeline.Annotation

	newSourceID string
	attachments []attachmentDraft
}

type attachmentDraft struct {
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

func (m model) deletedEventIDs() []string {
	var ids []string
	for _, e := range m.events {
		if e.deleted {
			ids = append(ids, e.ID)
		}
	}

	return ids
}

// seedStore writes m into a new database at path through the store API.
func seedStore(ctx context.Context, path string, m model) error {
	s, err := store.Open(ctx, path)
	if err != nil {
		return err
	}

	return errors.Join(writeModel(ctx, s, m), s.Close())
}

// writeModel deletes events last, so seqs have gaps and annotations, attachments and sources can
// lose what named them.
func writeModel(ctx context.Context, s *store.Store, m model) error {
	for _, t := range m.timelines {
		err := s.CreateTimeline(ctx, t)
		if err != nil {
			return err
		}
	}
	for _, e := range m.events {
		_, err := s.AddEvent(ctx, e.Event, e.newSourceID, newAttachments(e.attachments)...)
		if err != nil {
			return err
		}
	}
	for _, a := range m.annotations {
		err := s.AddAnnotation(ctx, a.Annotation, a.newSourceID, newAttachments(a.attachments)...)
		if err != nil {
			return err
		}
	}

	return s.DeleteEvents(ctx, m.deletedEventIDs()...)
}

func newAttachments(drafts []attachmentDraft) []store.NewAttachment {
	attachments := make([]store.NewAttachment, 0, len(drafts))
	for _, d := range drafts {
		attachments = append(attachments, store.NewAttachment{ID: d.id, Filename: d.name, CreatedAt: d.created, Content: bytes.NewReader(d.content)})
	}

	return attachments
}

// roundTrip exports the timelines named names from dir/a.db into dir/a/, imports that export into
// an empty dir/b.db, and exports b.db into dir/b/. It returns the first export, and an error unless
// both directories hold the same bytes.
func roundTrip(ctx context.Context, dir string, names []string) ([]byte, error) {
	c := coroner{home: dir, now: time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC), version: "(devel)"}

	_, err := c.run(ctx, exportArgs(filepath.Join(dir, "a.db"), names, filepath.Join(dir, "a")+string(filepath.Separator))...)
	if err != nil {
		return nil, err
	}
	jsonl, err := filepath.Glob(filepath.Join(dir, "a", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(jsonl) != 1 {
		return nil, fmt.Errorf("the export wrote %d JSONL files into %s, not one", len(jsonl), filepath.Join(dir, "a"))
	}
	_, err = c.run(ctx, "--db", filepath.Join(dir, "b.db"), "import", jsonl[0])
	if err != nil {
		return nil, err
	}
	_, err = c.run(ctx, exportArgs(filepath.Join(dir, "b.db"), names, filepath.Join(dir, "b")+string(filepath.Separator))...)
	if err != nil {
		return nil, err
	}

	differences, err := dirDiff(filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if err != nil {
		return nil, err
	}
	if differences != "" {
		return nil, fmt.Errorf("the export changed on its way through import:\n%s", differences)
	}

	return os.ReadFile(filepath.Clean(jsonl[0]))
}

func exportArgs(db string, names []string, into string) []string {
	args := []string{"--db", db, "timeline", "export", "--format", "jsonl", "-o", into}
	for _, name := range names {
		args = append(args, "-t", name)
	}

	return args
}

// coroner runs commands as the binary would, against real files, with a fixed clock and
// version and IDs numbered from 1 on every command.
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
			only := b
			if inA {
				only = a
			}
			fmt.Fprintf(&out, "%s is only in %s\n", name, only)
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
	fsys := os.DirFS(dir)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[path], err = fs.ReadFile(fsys, path)
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

// unseen returns what seen lacks of the five record types besides the header and, for each
// nullable key of its export type, that key both null and set.
func unseen(seen map[string]bool) []string {
	var missing []string
	for typ, record := range map[string]any{
		"timeline": export.Timeline{}, "source": export.Source{}, "event": export.Event{},
		"annotation": export.Annotation{}, "attachment": export.Attachment{},
	} {
		want := []string{typ}
		for _, key := range nullableKeys(typ, record) {
			want = append(want, key+"=null", key+"=set")
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

// nullableKeys returns typ.key for each pointer field of record, an export type. An exported event
// always has a seq.
func nullableKeys(typ string, record any) []string {
	var keys []string
	for field := range reflect.TypeOf(record).Fields() {
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Type.Kind() == reflect.Pointer && typ+"."+key != "event.seq" {
			keys = append(keys, typ+"."+key)
		}
	}

	return keys
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
			one := timelineModel(name, sources, nextID).Draw(t, "timeline")
			m.timelines = append(m.timelines, one.timelines...)
			m.events = append(m.events, one.events...)
			m.annotations = append(m.annotations, one.annotations...)
		}

		return m
	})
}

// timelineModel draws the timeline name with its events and annotations.
func timelineModel(name string, sources []string, nextID func() string) *rapid.Generator[model] {
	return rapid.Custom(func(t *rapid.T) model {
		tl := timeline.Timeline{
			ID: nextID(), Name: name, Title: blankable().Draw(t, "title"),
			Refs: rapid.SliceOfNDistinct(text(), 0, 3, rapid.ID).Draw(t, "refs"), CreatedAt: instant().Draw(t, "created"),
		}
		tl.UpdatedAt = tl.CreatedAt
		m := model{timelines: []timeline.Timeline{tl}}

		var eventIDs []string
		for range rapid.IntRange(0, 6).Draw(t, "events") {
			e := event(sources).Draw(t, "event")
			e.ID, e.TimelineID = nextID(), tl.ID
			eventIDs = append(eventIDs, e.ID)
			m.events = append(m.events, eventDraft{
				Event: e, newSourceID: nextID(), attachments: attachmentDrafts(nextID).Draw(t, "attachments"),
				deleted: rapid.Bool().Draw(t, "deleted"),
			})
		}
		for range rapid.IntRange(0, 4).Draw(t, "annotations") {
			a := annotation(sources, eventIDs).Draw(t, "annotation")
			a.ID, a.TimelineID = nextID(), tl.ID
			m.annotations = append(m.annotations, annotationDraft{Annotation: a, newSourceID: nextID(), attachments: attachmentDrafts(nextID).Draw(t, "attachments")})
		}

		return m
	})
}

// event draws an event without ids, its occurred_at at the start of its precision window in its
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
			OccurredRaw:    rapid.Ptr(blankable(), true).Draw(t, "raw"),
			Source:         optionalSource(sources).Draw(t, "source"),
			Tags:           rapid.SliceOfNDistinct(tag(), 0, 4, rapid.ID).Draw(t, "tags"),
			Text:           text().Draw(t, "text"),
			URL:            rapid.Ptr(blankable(), true).Draw(t, "url"),
			Author:         rapid.Ptr(blankable(), true).Draw(t, "author"),
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
			Author:    rapid.Ptr(blankable(), true).Draw(t, "author"),
			Source:    optionalSource(sources).Draw(t, "source"),
			URL:       rapid.Ptr(blankable(), true).Draw(t, "url"),
			CreatedAt: instant().Draw(t, "created"),
			UpdatedAt: instant().Draw(t, "updated"),
		}
		if len(eventIDs) > 0 {
			a.EventID = rapid.Ptr(rapid.SampledFrom(eventIDs), true).Draw(t, "event")
		}

		return a
	})
}

// attachmentDrafts draws up to two attachments, whose content often repeats so they share a blob.
func attachmentDrafts(nextID func() string) *rapid.Generator[[]attachmentDraft] {
	return rapid.Custom(func(t *rapid.T) []attachmentDraft {
		var out []attachmentDraft
		for range rapid.IntRange(0, 2).Draw(t, "count") {
			out = append(out, attachmentDraft{
				id: nextID(), name: blankable().Draw(t, "filename"), created: instant().Draw(t, "attached"),
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

// blankable draws text for a field with no not-blank rule, often empty or only whitespace.
func blankable() *rapid.Generator[string] {
	return rapid.OneOf(rapid.SampledFrom([]string{"", " ", "\t\n"}), text())
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
