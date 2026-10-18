// Command schemagen writes the JSON Schema of coroner's export format, generated from the
// internal/export types, to schema/export-v1.schema.json. With --check it reports whether
// that file is missing or differs instead, and exits 1 if so. Run it from the module root.
package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/macintacos/coroner/internal/export"
)

const (
	outPath = "schema/export-v1.schema.json"
	// exportDir is read for doc comments; invopop keys them by modulePath joined with it, so it
	// must be relative to the module root.
	exportDir  = "internal/export"
	modulePath = "github.com/macintacos/coroner"
)

func main() {
	checkOnly := flag.Bool("check", false, "report drift from "+outPath+" and exit 1 on any, writing nothing")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/tools/schemagen [--check]")
		os.Exit(2)
	}

	schema, err := generate()
	if err == nil {
		if *checkOnly {
			err = check(schema, outPath, os.Stderr)
		} else {
			err = write(schema, outPath)
		}
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

// generate returns the export schema, describing each object and field with its Go doc
// comment. The working directory must be the module root.
func generate() ([]byte, error) {
	reflector := &jsonschema.Reflector{AllowAdditionalProperties: true, ExpandedStruct: true, Mapper: instantSchema}

	err := reflector.AddGoComments(modulePath, exportDir, jsonschema.WithFullComment())
	if err != nil {
		return nil, err
	}

	schema := reflector.Reflect(export.Envelope{})
	schema.ID = jsonschema.ID("https://raw.githubusercontent.com/macintacos/coroner/main/" + outPath)
	tidyDescriptions(schema)
	for _, def := range schema.Definitions {
		tidyDescriptions(def)
	}

	var b bytes.Buffer
	err = json.MarshalWrite(&b, schema, json.Deterministic(true), jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}

	b.WriteByte('\n')

	return b.Bytes(), nil
}

// tidyDescriptions joins the wrapped lines of schema's description and of each property's, and
// moves a nullable property's description from inside its oneOf, where the nullable tag leaves
// it, onto the property.
func tidyDescriptions(schema *jsonschema.Schema) {
	schema.Description = strings.Join(strings.Fields(schema.Description), " ")
	if schema.Properties == nil {
		return
	}

	for property := range schema.Properties.ValuesFromOldest() {
		if len(property.OneOf) > 0 && property.Description == "" {
			property.Description, property.OneOf[0].Description = property.OneOf[0].Description, ""
		}
		property.Description = strings.Join(strings.Fields(property.Description), " ")
	}
}

func instantSchema(t reflect.Type) *jsonschema.Schema {
	if t != reflect.TypeFor[export.Instant]() {
		return nil
	}

	return &jsonschema.Schema{
		Type:    "string",
		Format:  "date-time",
		Pattern: `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{9}Z$`,
	}
}

func write(schema []byte, path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		return err
	}

	return os.WriteFile(path, schema, 0o600)
}

// check prints an added or changed line to out when the file at path differs from schema,
// and fails if it does.
func check(schema []byte, path string, out io.Writer) error {
	got, err := os.ReadFile(filepath.Clean(path))

	var drift string
	switch {
	case errors.Is(err, fs.ErrNotExist):
		drift = "added"
	case err != nil:
		return err
	case !bytes.Equal(got, schema):
		drift = "changed"
	default:
		return nil
	}

	_, err = fmt.Fprintf(out, "%s: %s\n", drift, path)

	return errors.Join(err, fmt.Errorf("%s is out of date; run `go run ./internal/tools/schemagen`", path))
}
