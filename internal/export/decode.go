package export

import (
	"bufio"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/google/uuid"

	"github.com/macintacos/coroner/internal/timeline"
)

// shownLineErrors is how many invalid lines a DecodeError's message lists.
const shownLineErrors = 5

// LineError is why Decode rejected one line of its input.
type LineError struct {
	// Line counts from 1.
	Line int
	Err  error
}

func (e LineError) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e LineError) Unwrap() error {
	return e.Err
}

// DecodeError is every line Decode rejected, in input order.
type DecodeError struct {
	Lines []LineError
}

// Error returns a count, the first few invalid lines and how many more there are.
func (e *DecodeError) Error() string {
	messages := []string{fmt.Sprintf("invalid JSONL lines: %d", len(e.Lines))}
	for _, line := range e.Lines[:min(len(e.Lines), shownLineErrors)] {
		messages = append(messages, line.Error())
	}
	if more := len(e.Lines) - shownLineErrors; more > 0 {
		messages = append(messages, fmt.Sprintf("and %d more", more))
	}

	return strings.Join(messages, "\n")
}

// Decode reads JSONL records from r, one per line, of any length. It returns the records of
// every valid line in order, and a *DecodeError when any line is invalid. A line is invalid
// when it lacks a key of its record type, holds null where that key never does, or has a
// malformed ID, instant, name or tag. Keys and record types Decode does not know are ignored.
// A header of an unsupported major version is invalid, and the records after it are skipped
// until the next header; records before any header are read as this version. A failure to
// read r is returned as it is.
func Decode(r io.Reader) ([]Record, error) {
	reader := bufio.NewReader(r)
	var d decoder
	for n := 1; ; n++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			d.add(n, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return d.records, err
		}
	}

	if len(d.invalid) > 0 {
		return d.records, &DecodeError{Lines: d.invalid}
	}

	return d.records, nil
}

type decoder struct {
	records []Record
	invalid []LineError
	// skipping is set by a rejected header and cleared by the next valid one.
	skipping bool
}

func (d *decoder) add(n int, line []byte) {
	typ, record, err := decodeLine(line)
	if typ == "header" {
		d.skipping = err != nil
	} else if d.skipping {
		return
	}

	switch {
	case err != nil:
		d.invalid = append(d.invalid, LineError{Line: n, Err: err})
	case record != nil:
		d.records = append(d.records, record)
	}
}

// decodeLine returns the line's record type and its record, which is nil for a type Decode
// does not know.
func decodeLine(line []byte) (string, Record, error) {
	var members map[string]jsontext.Value
	err := json.Unmarshal(line, &members)
	if err != nil {
		return "", nil, err
	}
	raw, ok := members["type"]
	if !ok {
		return "", nil, errors.New(`missing key "type"`)
	}
	var typ string
	err = json.Unmarshal(raw, &typ)
	if err != nil {
		return "", nil, fmt.Errorf(`key "type": %w`, err)
	}

	var record Record
	switch typ {
	case "header":
		record, err = decodeRecord[Header](line, members)
	case "timeline":
		record, err = decodeRecord[Timeline](line, members)
	case "source":
		record, err = decodeRecord[Source](line, members)
	case "event":
		record, err = decodeRecord[Event](line, members)
	case "annotation":
		record, err = decodeRecord[Annotation](line, members)
	}

	return typ, record, err
}

func decodeRecord[T Record](line []byte, members map[string]jsontext.Value) (Record, error) {
	var record T
	err := checkKeys(members, reflect.TypeFor[T]())
	if err == nil {
		err = json.Unmarshal(line, &record)
	}
	if err == nil {
		err = record.validate()
	}

	return record, err
}

// checkKeys returns an error unless members holds every JSON key of struct type t, and of the
// query inside it, with null only where t's field is a pointer.
func checkKeys(members map[string]jsontext.Value, t reflect.Type) error {
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		value, ok := members[name]
		switch {
		case !ok:
			return fmt.Errorf("missing key %q", name)
		case value.Kind() == 'n' && field.Type.Kind() != reflect.Pointer:
			return fmt.Errorf("key %q must not be null", name)
		case field.Type == reflect.TypeFor[Query]():
			var query map[string]jsontext.Value
			err := json.Unmarshal(value, &query)
			if err == nil {
				err = checkKeys(query, field.Type)
			}
			if err != nil {
				return fmt.Errorf("key %q: %w", name, err)
			}
		}
	}

	return nil
}

func (h Header) validate() error {
	if h.Format != Format {
		return fmt.Errorf("format %q is not %q", h.Format, Format)
	}
	major, _, _ := strings.Cut(h.FormatVersion, ".")
	supported, _, _ := strings.Cut(FormatVersion, ".")
	if major != supported {
		return fmt.Errorf("format_version %q is not supported; this coroner reads %s.x", h.FormatVersion, supported)
	}

	return errors.Join(
		validateEach(timeline.ValidateTimelineName, h.Query.Timelines),
		validateEach(timeline.ValidateSourceName, h.Query.Sources),
		validateEach(timeline.ValidateTag, h.Query.Tags),
	)
}

func (t Timeline) validate() error {
	return errors.Join(validateID("id", t.ID), timeline.ValidateTimelineName(t.Name))
}

func (s Source) validate() error {
	return errors.Join(validateID("id", s.ID), timeline.ValidateSourceName(s.Name))
}

func (e Event) validate() error {
	return errors.Join(
		validateID("id", e.ID),
		validateID("timeline_id", e.TimelineID),
		validateOptional(timeline.ValidateSourceName, e.Source),
		validateEach(timeline.ValidateTag, e.Tags),
	)
}

func (a Annotation) validate() error {
	return errors.Join(
		validateID("id", a.ID),
		validateID("timeline_id", a.TimelineID),
		validateOptional(func(id string) error { return validateID("event_id", id) }, a.EventID),
		validateOptional(timeline.ValidateSourceName, a.Source),
	)
}

// validateID returns an error unless id is a UUID written as lowercase hex with hyphens.
func validateID(key, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return fmt.Errorf("%s %q: must be a lowercase UUID", key, id)
	}

	return nil
}

func validateOptional(validate func(string) error, s *string) error {
	if s == nil {
		return nil
	}

	return validate(*s)
}

func validateEach(validate func(string) error, values []string) error {
	errs := make([]error, 0, len(values))
	for _, value := range values {
		errs = append(errs, validate(value))
	}

	return errors.Join(errs...)
}
