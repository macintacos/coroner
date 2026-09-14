package export_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/macintacos/coroner/internal/export"
)

const fixturesDir = "../../testdata/export"

var _ = Describe("Frozen export fixtures", func() {
	// Every file under v*/: the first spec fails when a fixture is added or removed without this list.
	frozen := []string{
		"v1.0/annotations.jsonl",
		"v1.0/concatenated.jsonl",
		"v1.0/full-model.json",
		"v1.0/full-model.jsonl",
		"v1.0/hostile-text.jsonl",
		"v1.0/minimal.jsonl",
		"v1.0/ordering.jsonl",
		"v1.1/annotations.jsonl",
		"v1.1/attachments.json",
		"v1.1/attachments.jsonl",
		"v1.1/concatenated.jsonl",
		"v1.1/full-model.json",
		"v1.1/full-model.jsonl",
		"v1.1/hostile-text.jsonl",
		"v1.1/minimal.jsonl",
		"v1.1/ordering.jsonl",
	}
	entries := make([]TableEntry, 0, len(frozen))
	var currentEntries []TableEntry
	for _, name := range frozen {
		entry := Entry(name, name)
		entries = append(entries, entry)
		if strings.HasPrefix(name, "v"+export.FormatVersion+"/") {
			currentEntries = append(currentEntries, entry)
		}
	}

	It("lists every file in a version directory", func() {
		paths, err := filepath.Glob(filepath.Join(fixturesDir, "v*", "*"))
		paths = slices.DeleteFunc(paths, func(path string) bool { return strings.HasPrefix(filepath.Base(path), ".") })

		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(ConsistOf(fixturePaths(frozen)))
	})

	DescribeTable("validates against the export schema, a JSONL line against its type's definition",
		func(name string) {
			schemas := exportSchemas()
			if filepath.Ext(name) == ".json" {
				Expect(schemas["envelope"].Validate(schemaValue(readFixture(name)))).To(Succeed())
				return
			}

			for i, line := range slices.Collect(bytes.Lines(readFixture(name))) {
				record, ok := schemaValue(line).(map[string]any)
				Expect(ok).To(BeTrue(), "line %d is an object", i+1)
				typ, _ := record["type"].(string)
				if typ == "header" {
					// A header is the envelope without its record lists.
					typ = "envelope"
					for _, key := range []string{"timelines", "sources", "events", "annotations"} {
						record[key] = []any{}
					}
				}
				Expect(schemas).To(HaveKey(typ), "line %d", i+1)
				Expect(schemas[typ].Validate(record)).To(Succeed(), "line %d", i+1)
			}
		},
		entries,
	)

	DescribeTable("decodes every record with zero errors",
		func(name string) {
			data := readFixture(name)
			if filepath.Ext(name) == ".json" {
				var env export.Envelope
				Expect(json.Unmarshal(data, &env, json.RejectUnknownMembers(true))).To(Succeed())
				return
			}

			records, err := export.Decode(bytes.NewReader(data))

			Expect(err).NotTo(HaveOccurred())
			Expect(records).To(HaveLen(bytes.Count(data, []byte("\n"))))
		},
		entries,
	)

	DescribeTable("re-encodes byte-identically",
		func(name string) {
			data := readFixture(name)

			Expect(string(reencode(name, data))).To(Equal(string(data)))
		},
		currentEntries,
	)

	// TODO: EXC-1284 makes this a store-backed property test.
	PIt("round-trips each fixture through import into an empty store and export again (EXC-1284)")
})

var _ = Describe("Compatibility fixtures", func() {
	It("decodes a later minor version, ignoring the keys, record types and enum values it adds", func() {
		records, err := export.Decode(bytes.NewReader(readFixture("compat/v1.2-unknown-keys-and-types.jsonl")))

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{
			"header", "timeline 01a09862-b1c0-7000-8000-000000000002", "event 01a09862-b1c0-7000-8000-000000000009",
		}))
	})

	It("rejects major version 2, naming the major it reads, and skips its records", func() {
		records, err := export.Decode(bytes.NewReader(readFixture("compat/v2.0.jsonl")))

		Expect(err).To(MatchError(And(ContainSubstring(`"2.0"`), ContainSubstring("1.x"))))
		Expect(records).To(BeEmpty())
	})
})

func readFixture(name string) []byte {
	data, err := os.ReadFile(filepath.Join(fixturesDir, filepath.Clean(name)))
	Expect(err).NotTo(HaveOccurred())

	return data
}

func fixturePaths(names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(fixturesDir, name))
	}

	return paths
}

// exportSchemas returns the export schema compiled for the envelope and for each JSONL record
// type but the header.
func exportSchemas() map[string]*jsonschema.Schema {
	path, err := filepath.Abs("../../schema/export-v1.schema.json")
	Expect(err).NotTo(HaveOccurred())
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	schemas := map[string]*jsonschema.Schema{}
	for typ, pointer := range map[string]string{
		"envelope": "", "timeline": "#/$defs/Timeline", "source": "#/$defs/Source", "event": "#/$defs/Event",
		"annotation": "#/$defs/Annotation", "attachment": "#/$defs/Attachment",
	} {
		schemas[typ], err = compiler.Compile(path + pointer)
		Expect(err).NotTo(HaveOccurred())
	}

	return schemas
}

// schemaValue decodes JSON as the validator reads it.
func schemaValue(data []byte) any {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	Expect(err).NotTo(HaveOccurred())

	return value
}

// reencode decodes a fixture and encodes it again: a JSON document as it was decoded, and JSONL
// as export.Records orders each export in it, so a change to the record order changes the bytes.
func reencode(name string, data []byte) []byte {
	var out bytes.Buffer
	if filepath.Ext(name) == ".json" {
		var env export.Envelope
		Expect(json.Unmarshal(data, &env)).To(Succeed())
		Expect(export.Write(&out, env)).To(Succeed())

		return out.Bytes()
	}

	records, err := export.Decode(bytes.NewReader(data))
	Expect(err).NotTo(HaveOccurred())
	for _, env := range envelopes(records) {
		ordered, err := export.Records(env)
		Expect(err).NotTo(HaveOccurred())
		Expect(export.WriteRecords(&out, ordered)).To(Succeed())
	}

	return out.Bytes()
}

// envelopes returns an envelope for each header in records, holding the records up to the next.
func envelopes(records []export.Record) []export.Envelope {
	var envs []export.Envelope
	for _, record := range records {
		if header, ok := record.(export.Header); ok {
			envs = append(envs, export.Envelope{
				Format: header.Format, FormatVersion: header.FormatVersion, CoronerVersion: header.CoronerVersion, ExportedAt: header.ExportedAt, Query: header.Query,
			})
			continue
		}

		Expect(envs).NotTo(BeEmpty(), "a record before any header")
		env := &envs[len(envs)-1]
		switch record := record.(type) {
		case export.Timeline:
			env.Timelines = append(env.Timelines, record)
		case export.Source:
			env.Sources = append(env.Sources, record)
		case export.Event:
			env.Events = append(env.Events, record)
		case export.Annotation:
			env.Annotations = append(env.Annotations, record)
		case export.Attachment:
			env.Attachments = append(env.Attachments, record)
		}
	}

	return envs
}
