package export

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/macintacos/coroner/internal/timeline"
)

// shownLineErrors is how many invalid lines a DecodeError's message lists.
const shownLineErrors = 5

// errUnsupported marks a header whose records Decode skips.
var errUnsupported = errors.New("unsupported")

// LineError is why Decode rejected one line of its input.
type LineError struct {
	// Line counts from 1.
	Line int
	Err  error
}

func (e LineError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, strings.ReplaceAll(e.Err.Error(), "\n", "; "))
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
	header := fmt.Sprintf("%d lines are not valid export records:", len(e.Lines))
	if len(e.Lines) == 1 {
		header = "1 line is not a valid export record:"
	}
	messages := []string{header}
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
// when it is not a JSON object with a string "type"; when its record lacks a key, holds null
// where the key never does, has a value of the wrong type, or has a malformed ID, instant,
// name or tag; or when it is a header with an unsupported format or format_version. Keys and
// record types Decode does not know are ignored. The records after an unsupported header are
// skipped until the next header; records before any header are read as this version. A
// failure to read r is returned as it is.
func Decode(r io.Reader) ([]Record, error) {
	reader := bufio.NewReader(r)
	var d decoder
	for lineNum := 1; ; lineNum++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			d.add(lineNum, line)
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
	// skipping is set by an unsupported header and cleared by the next supported one.
	skipping bool
}

func (d *decoder) add(lineNum int, line []byte) {
	typ, record, err := decodeLine(line)
	if typ == "header" {
		d.skipping = errors.Is(err, errUnsupported)
	} else if d.skipping {
		return
	}

	switch {
	case err != nil:
		d.invalid = append(d.invalid, LineError{Line: lineNum, Err: err})
	case record != nil:
		d.records = append(d.records, record)
	}
}

// decodeLine returns the line's record type and its record, which is nil for a type Decode
// does not know.
func decodeLine(line []byte) (string, Record, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return "", nil, errors.New("blank line, which JSON Lines does not allow")
	}
	var members map[string]jsontext.Value
	err := json.Unmarshal(line, &members)
	if err != nil {
		return "", nil, plain(err)
	}
	raw, ok := members["type"]
	if !ok {
		return "", nil, errors.New(`missing key "type"`)
	}
	var typ string
	err = json.Unmarshal(raw, &typ)
	if err != nil || raw.Kind() != '"' {
		return "", nil, errors.New(`key "type": must be a string`)
	}

	var record Record
	switch typ {
	case "header":
		record, err = decodeHeader(line, members)
	case "timeline":
		record, err = decodeRecord[Timeline](line, members)
	case "source":
		record, err = decodeRecord[Source](line, members)
	case "event":
		record, err = decodeRecord[Event](line, members)
	case "annotation":
		record, err = decodeRecord[Annotation](line, members)
	case "attachment":
		record, err = decodeRecord[Attachment](line, members)
	}

	return typ, record, err
}

func decodeRecord[T Record](line []byte, members map[string]jsontext.Value) (Record, error) {
	var record T
	err := checkKeys(members, reflect.TypeFor[T]())
	if err == nil {
		err = plain(json.Unmarshal(line, &record))
	}
	if err == nil {
		err = record.validate()
	}

	return record, err
}

// decodeHeader checks format and format_version before the keys that a header of another
// version may change.
func decodeHeader(line []byte, members map[string]jsontext.Value) (Record, error) {
	var version struct {
		Format        string `json:"format"`
		FormatVersion string `json:"format_version"`
	}
	err := plain(json.Unmarshal(line, &version))
	if err == nil {
		err = checkSupported(version.Format, version.FormatVersion)
	}
	if err != nil {
		return nil, err
	}

	return decodeRecord[Header](line, members)
}

// checkSupported returns an error wrapping errUnsupported unless format is Format and
// formatVersion is a MAJOR.MINOR of FormatVersion's major.
func checkSupported(format, formatVersion string) error {
	if format != Format {
		return fmt.Errorf("%w format %q; this coroner reads %q", errUnsupported, format, Format)
	}
	major, minor, _ := strings.Cut(formatVersion, ".")
	supported, _, _ := strings.Cut(FormatVersion, ".")
	_, err := strconv.ParseUint(minor, 10, 64)
	if major != supported || err != nil {
		return fmt.Errorf("%w format_version %q; this coroner reads %s.x", errUnsupported, formatVersion, supported)
	}

	return nil
}

// plain rewords a json/v2 semantic error, whose text names Go types, by the key it is at.
func plain(err error) error {
	semantic, ok := errors.AsType[*json.SemanticError](err)
	if !ok {
		return err
	}
	key := strings.TrimPrefix(string(semantic.JSONPointer), "/")
	switch {
	case key == "":
		return errors.New("not a JSON object")
	case semantic.Err != nil:
		return fmt.Errorf("key %q: %w", key, semantic.Err)
	default:
		return fmt.Errorf("key %q: must not be a JSON %s", key, kindName(semantic.JSONKind))
	}
}

// kindName names a JSON kind as its JSON type.
func kindName(kind jsontext.Kind) string {
	switch kind {
	case jsontext.KindBeginObject, jsontext.KindEndObject:
		return "object"
	case jsontext.KindBeginArray, jsontext.KindEndArray:
		return "array"
	case jsontext.KindTrue, jsontext.KindFalse:
		return "boolean"
	case jsontext.KindInvalid, jsontext.KindNull, jsontext.KindString, jsontext.KindNumber:
	}

	return kind.String()
}

// checkKeys returns an error unless members holds every JSON key of struct type t, and of the
// query inside it, with null only where t's field is a pointer. It requires every key of the
// current type, so a key added in a minor version must be exempted for exports of older minors.
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
			err := plain(json.Unmarshal(value, &query))
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

func (a Attachment) validate() error {
	owned := timeline.Attachment{Owner: timeline.AttachmentOwner{EventID: a.EventID, AnnotationID: a.AnnotationID}, SHA256: a.SHA256, Size: a.Size}

	return errors.Join(
		validateID("id", a.ID),
		validateOptional(func(id string) error { return validateID("event_id", id) }, a.EventID),
		validateOptional(func(id string) error { return validateID("annotation_id", id) }, a.AnnotationID),
		owned.Validate(),
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
