package timeline

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Precision is how exact an event's occurred time is.
type Precision string

// Precisions, coarsest first. Their values are stored and exported verbatim.
const (
	PrecisionDay         Precision = "day"
	PrecisionHour        Precision = "hour"
	PrecisionMinute      Precision = "minute"
	PrecisionSecond      Precision = "second"
	PrecisionMillisecond Precision = "millisecond"
	PrecisionMicrosecond Precision = "microsecond"
	PrecisionNanosecond  Precision = "nanosecond"
)

// AnnotationKind says whether an annotation is raw material or investigator commentary.
type AnnotationKind string

// Annotation kinds. Their values are stored and exported verbatim.
const (
	AnnotationEvidence AnnotationKind = "evidence"
	AnnotationComment  AnnotationKind = "comment"
)

// Timeline is the named chronology of one investigation.
type Timeline struct {
	ID        string
	Name      string
	Title     string
	Refs      []string // ticket keys or URLs, never resolved
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Event is something that happened at a time, or over an interval.
type Event struct {
	ID         string
	TimelineID string
	Seq        int64
	// OccurredAt is the start of its Precision window, truncated in the observed zone.
	OccurredAt time.Time
	// OccurredOffset is seconds east of UTC as the input stated it; nil when the input was
	// Z or an epoch, which say nothing about the local offset.
	OccurredOffset *int
	Precision      Precision
	Approximate    bool
	OccurredRaw    *string
	EndedAt        *time.Time
	Source         *string
	Tags           []string
	Text           string
	URL            *string
	Author         *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
	Source     *string
	URL        *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Source is the system, channel or person an event or annotation came from.
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
	var kindErr error
	if a.Kind != AnnotationEvidence && a.Kind != AnnotationComment {
		kindErr = fmt.Errorf("annotation kind %q: must be %q or %q", a.Kind, AnnotationEvidence, AnnotationComment)
	}

	return errors.Join(
		kindErr,
		validateNotBlank("body", a.Body),
		validateOptional(ValidateSourceName, a.Source),
	)
}

// Validate reports whether s breaks the domain rules.
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

func validateOccurredAt(at time.Time, offset *int, precision Precision) error {
	if !slices.Contains([]Precision{
		PrecisionDay, PrecisionHour, PrecisionMinute, PrecisionSecond,
		PrecisionMillisecond, PrecisionMicrosecond, PrecisionNanosecond,
	}, precision) {
		return fmt.Errorf("precision %q: must be day, hour, minute, second, millisecond, microsecond or nanosecond", precision)
	}

	err := validateInstantRange("occurred at", at)
	if err != nil {
		return err
	}

	zone := time.UTC
	if offset != nil {
		zone = time.FixedZone("", *offset)
	}
	if !precision.Truncate(at.In(zone)).Equal(at) {
		return fmt.Errorf("occurred at %s: must be the start of its %s", FormatInstant(at), precision)
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
