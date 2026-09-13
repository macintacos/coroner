package timeline

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AnnotationKind says whether an annotation is raw material or investigator commentary.
type AnnotationKind string

// Annotation kinds. Their values are stored and exported verbatim.
const (
	AnnotationEvidence AnnotationKind = "evidence"
	AnnotationComment  AnnotationKind = "comment"
)

func (k AnnotationKind) valid() bool {
	switch k {
	case AnnotationEvidence, AnnotationComment:
		return true
	}

	return false
}

// Timeline is the named chronology of one investigation.
type Timeline struct {
	ID string
	// Name is the grammar-checked name the CLI selects a timeline by; see ValidateTimelineName.
	Name string
	// Title is a free-text heading; it may be empty.
	Title string
	// Refs are issue-tracker keys or URLs, kept as written.
	Refs      []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Event is something that happened at a time, or over an interval.
type Event struct {
	ID         string
	TimelineID string
	Seq        int64
	// OccurredAt is the start of its Precision window, measured in the offset OccurredOffset
	// states (UTC when nil).
	OccurredAt time.Time
	// OccurredOffset is seconds east of UTC as the input stated it; nil when the input was
	// Z or an epoch, which say nothing about the local offset.
	OccurredOffset *int
	Precision      Precision
	Approximate    bool
	OccurredRaw    *string
	EndedAt        *time.Time
	// Source is the Name of the Source the event came from; nil when unknown.
	Source *string
	Tags   []string
	Text   string
	// URL is a permalink to the thing itself, such as the log line or chat message; nil when
	// none.
	URL       *string
	Author    *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Annotation is evidence or commentary attached to an event, or to its timeline when
// EventID is nil.
type Annotation struct {
	ID         string
	TimelineID string
	EventID    *string
	Kind       AnnotationKind
	Body       string
	Author     *string
	// Source is the Name of the Source the annotation came from; nil when unknown.
	Source *string
	// URL is a permalink to the thing itself, such as the log line or chat message; nil when
	// none.
	URL       *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Source is the system, service or person an event or annotation came from.
type Source struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Validate reports every way t breaks the domain rules, or nil.
func (t Timeline) Validate() error {
	errs := []error{ValidateTimelineName(t.Name)}
	for i, ref := range t.Refs {
		errs = append(errs, validateNotBlank(fmt.Sprintf("ref %d", i+1), ref))
	}

	return errors.Join(errs...)
}

// Validate reports every way e breaks the domain rules, or nil.
func (e Event) Validate() error {
	errs := []error{
		validateNotBlank("text", e.Text),
		validatePrecision(e.Precision),
		validateOccurredAt(e.OccurredAt, e.OccurredOffset, e.Precision),
		validateEndedAt(e.EndedAt, e.OccurredAt),
		validateOptional(ValidateSourceName, e.Source),
	}
	for _, tag := range e.Tags {
		errs = append(errs, ValidateTag(tag))
	}

	return errors.Join(errs...)
}

// Validate reports every way a breaks the domain rules, or nil.
func (a Annotation) Validate() error {
	return errors.Join(
		validateAnnotationKind(a.Kind),
		validateNotBlank("body", a.Body),
		validateOptional(ValidateSourceName, a.Source),
	)
}

// Validate reports every way s breaks the domain rules, or nil.
func (s Source) Validate() error {
	return ValidateSourceName(s.Name)
}

// CompareEvents orders events by occurred at, then seq, for [slices.SortFunc]. A coarse
// event sorts at the start of its window, so "10:15" precedes every 10:15:xx event.
func CompareEvents(a, b Event) int {
	return cmp.Or(a.OccurredAt.Compare(b.OccurredAt), cmp.Compare(a.Seq, b.Seq))
}

func validateNotBlank(what, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s: must not be blank", what)
	}

	return nil
}

func validateOptional(validate func(string) error, s *string) error {
	if s == nil {
		return nil
	}

	return validate(*s)
}

func validateAnnotationKind(kind AnnotationKind) error {
	if kind.valid() {
		return nil
	}

	return fmt.Errorf("annotation kind %q: must be %q or %q", kind, AnnotationEvidence, AnnotationComment)
}

func validatePrecision(precision Precision) error {
	if precision.valid() {
		return nil
	}

	return fmt.Errorf("precision %q: must be day, hour, minute, second, millisecond, microsecond or nanosecond", precision)
}

func validateOccurredAt(at time.Time, offset *int, precision Precision) error {
	err := validateInstantRange("occurred at", at)
	if err != nil || !precision.valid() {
		return err
	}

	zone := time.UTC
	if offset != nil {
		zone = time.FixedZone("", *offset)
	}
	if !precision.Truncate(at.In(zone)).Equal(at) {
		return fmt.Errorf("occurred at %s: must be the start of its %s", at.In(zone).Format(time.RFC3339Nano), precision)
	}

	return nil
}

func validateEndedAt(ended *time.Time, occurred time.Time) error {
	if ended == nil {
		return nil
	}

	err := validateInstantRange("ended at", *ended)
	if err != nil {
		return err
	}
	if ended.Before(occurred) {
		return fmt.Errorf("ended at %s: must not be before occurred at %s", FormatInstant(*ended), FormatInstant(occurred))
	}

	return nil
}

func validateInstantRange(what string, t time.Time) error {
	if year := t.UTC().Year(); year < 0 || year > 9999 {
		return fmt.Errorf("%s: year %d is outside 0000–9999", what, year)
	}

	return nil
}
