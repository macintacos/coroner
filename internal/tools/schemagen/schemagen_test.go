package main

import (
	"bytes"
	"encoding/json/v2"
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
		Expect(string(schema)).NotTo(ContainSubstring(`"additionalProperties": false`))
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
				name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
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

	It("describes objects and fields with their Go doc comments", func() {
		event := definitions(schema)[reflect.TypeFor[export.Event]()]

		Expect(event["description"]).To(HavePrefix("Event is something that happened"))
		Expect(property(event, "occurred_offset")["description"]).NotTo(BeEmpty())
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
