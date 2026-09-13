package main

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

type object = map[string]any

// definitions returns each object schema in the generated schema by the Go type it describes.
func definitions(schema []byte) map[reflect.Type]object {
	var root object
	Expect(json.Unmarshal(schema, &root)).To(Succeed())
	defs, ok := root["$defs"].(object)
	Expect(ok).To(BeTrue(), "$defs")

	// Listing $defs exactly makes a new definition fail here until it joins byType, so every
	// spec that ranges over definitions covers it.
	Expect(slices.Sorted(maps.Keys(defs))).To(Equal([]string{"Annotation", "Event", "Query", "Source", "Timeline"}))

	byType := map[reflect.Type]object{reflect.TypeFor[export.Envelope](): root}
	for _, t := range []reflect.Type{
		reflect.TypeFor[export.Query](), reflect.TypeFor[export.Timeline](), reflect.TypeFor[export.Source](),
		reflect.TypeFor[export.Event](), reflect.TypeFor[export.Annotation](),
	} {
		def, ok := defs[t.Name()].(object)
		Expect(ok).To(BeTrue(), t.Name())
		byType[t] = def
	}

	return byType
}

// objects returns every JSON object in the decoded schema node, at any depth.
func objects(node any) []object {
	var found []object
	switch v := node.(type) {
	case object:
		found = append(found, v)
		for _, child := range v {
			found = append(found, objects(child)...)
		}
	case []any:
		for _, child := range v {
			found = append(found, objects(child)...)
		}
	}

	return found
}

func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return name
}

// docKeys returns the keys the reference doc's table lists under heading, in order.
func docKeys(doc, heading string) []string {
	_, section, found := strings.Cut(doc, "\n"+heading+"\n")
	Expect(found).To(BeTrue(), heading)
	if end := regexp.MustCompile(`(?m)^#`).FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}

	var keys []string
	for _, match := range regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|").FindAllStringSubmatch(section, -1) {
		keys = append(keys, match[1])
	}

	return keys
}

func property(def object, name string) object {
	properties, ok := def["properties"].(object)
	Expect(ok).To(BeTrue(), "properties")
	prop, ok := properties[name].(object)
	Expect(ok).To(BeTrue(), name)

	return prop
}

func allowsNull(prop object) bool {
	oneOf, _ := prop["oneOf"].([]any)

	return slices.ContainsFunc(oneOf, func(alt any) bool {
		schema, _ := alt.(object)
		return schema["type"] == "null"
	})
}

var _ = Describe("schemagen", func() {
	var schema []byte

	BeforeEach(func() {
		GinkgoT().Chdir(filepath.Join("..", "..", ".."))

		var err error
		schema, err = generate()
		Expect(err).NotTo(HaveOccurred())
	})

	It("generates the same bytes every time", func() {
		Expect(generate()).To(Equal(schema))
	})

	It("is a draft 2020-12 schema of the envelope, identified by its published URL", func() {
		var root object
		Expect(json.Unmarshal(schema, &root)).To(Succeed())

		Expect(root).To(HaveKeyWithValue("$schema", "https://json-schema.org/draft/2020-12/schema"))
		Expect(root).To(HaveKeyWithValue("$id", "https://raw.githubusercontent.com/macintacos/coroner/main/schema/export-v1.schema.json"))
		Expect(property(root, "format_version")).To(HaveKeyWithValue("type", "string"))
	})

	It("lets every object carry properties it does not list", func() {
		var root any
		Expect(json.Unmarshal(schema, &root)).To(Succeed())

		for _, obj := range objects(root) {
			Expect(obj).NotTo(HaveKeyWithValue("additionalProperties", false))
		}
	})

	It("requires every property of every object", func() {
		for t, def := range definitions(schema) {
			properties, _ := def["properties"].(object)
			var names []any
			for name := range properties {
				names = append(names, name)
			}

			Expect(def["required"]).To(ConsistOf(names), t.Name())
		}
	})

	It("allows null for exactly the fields that are pointers", func() {
		for t, def := range definitions(schema) {
			for field := range t.Fields() {
				name := jsonName(field)
				Expect(allowsNull(property(def, name))).To(Equal(field.Type.Kind() == reflect.Pointer), t.Name()+"."+name)
			}
		}
	})

	It("describes an instant by a pattern the canonical instant matches and a trimmed RFC 3339 time does not", func() {
		pattern, ok := property(definitions(schema)[reflect.TypeFor[export.Event]()], "occurred_at")["pattern"].(string)
		Expect(ok).To(BeTrue())
		at := time.Date(2026, 9, 12, 14, 5, 3, 0, time.UTC)

		Expect(timeline.FormatInstant(at)).To(MatchRegexp(pattern))
		Expect(regexp.MustCompile(pattern).MatchString(at.Format(time.RFC3339Nano))).To(BeFalse())
	})

	It("describes objects with their whole Go doc comment, unwrapped", func() {
		byType := definitions(schema)
		var root any
		Expect(json.Unmarshal(schema, &root)).To(Succeed())

		Expect(byType[reflect.TypeFor[export.Event]()]["description"]).To(HavePrefix("Event is something that happened"))
		Expect(byType[reflect.TypeFor[export.Query]()]["description"]).To(ContainSubstring("null or an empty list"))
		for _, obj := range objects(root) {
			if description, ok := obj["description"].(string); ok {
				Expect(description).NotTo(ContainSubstring("\n"))
			}
		}
	})

	It("describes every property with its Go doc comment", func() {
		for t, def := range definitions(schema) {
			properties, _ := def["properties"].(object)
			for name, prop := range properties {
				Expect(prop).To(HaveKeyWithValue("description", Not(BeEmpty())), t.Name()+"."+name)
			}
		}
	})

	It("lists each object's keys in the export format reference, in field order", func() {
		doc, err := os.ReadFile("docs/reference/export-format.md")
		Expect(err).NotTo(HaveOccurred())
		headings := map[reflect.Type]string{
			reflect.TypeFor[export.Envelope](): "## The envelope", reflect.TypeFor[export.Query](): "### query",
			reflect.TypeFor[export.Timeline](): "### Timeline", reflect.TypeFor[export.Source](): "### Source",
			reflect.TypeFor[export.Event](): "### Event", reflect.TypeFor[export.Annotation](): "### Annotation",
		}

		for t := range definitions(schema) {
			Expect(headings).To(HaveKey(t), t.Name())
			var names []string
			for field := range t.Fields() {
				names = append(names, jsonName(field))
			}

			Expect(docKeys(string(doc), headings[t])).To(Equal(names), t.Name())
		}
	})

	Describe("check", func() {
		var path string

		BeforeEach(func() {
			path = filepath.Join(GinkgoT().TempDir(), "schema", "export-v1.schema.json")
			Expect(write(schema, path)).To(Succeed())
		})

		It("accepts a file that matches the types, printing nothing", func() {
			var out bytes.Buffer

			Expect(check(schema, path, &out)).To(Succeed())
			Expect(out.String()).To(BeEmpty())
		})

		It("reports a changed file and fails, naming the command that regenerates it", func() {
			Expect(os.WriteFile(path, append(bytes.Clone(schema), ' '), 0o600)).To(Succeed())
			var out bytes.Buffer

			Expect(check(schema, path, &out)).To(MatchError(ContainSubstring("go run ./internal/tools/schemagen")))
			Expect(out.String()).To(Equal("changed: " + path + "\n"))
		})

		It("reports a missing file and fails", func() {
			Expect(os.Remove(path)).To(Succeed())
			var out bytes.Buffer

			Expect(check(schema, path, &out)).NotTo(Succeed())
			Expect(out.String()).To(Equal("added: " + path + "\n"))
		})
	})
})
