package timeline

import (
	"cmp"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// AnnotationKind says whether an annotation is raw material or investigator commentary.
type AnnotationKind string

// Annotation kinds. Their values are stored and exported verbatim.
const (
	AnnotationEvidence AnnotationKind = "evidence"
	AnnotationComment  AnnotationKind = "comment"
)

// Valid reports whether k is a known annotation kind.
func (k AnnotationKind) Valid() bool {
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
	errs := []error{ValidateTimelineName(t.Name), ValidateUTF8("title", t.Title)}
	for i, ref := range t.Refs {
		what := fmt.Sprintf("ref %d", i+1)
		errs = append(errs, validateNotBlank("refs", what, ref), validateUTF8("refs", what, ref))
	}

	return join(errs...)
}

// Validate reports every way e breaks the domain rules, or nil.
func (e Event) Validate() error {
	errs := []error{
		ValidateText("text", e.Text),
		validatePrecision(e.Precision),
		validateOccurredAt(e.OccurredAt, e.OccurredOffset, e.Precision),
		ValidateOccurredOffset(e.OccurredOffset),
		validateOptionalUTF8("occurred_raw", e.OccurredRaw),
		ValidateEndedAt(e.EndedAt, e.OccurredAt),
		validateOptional(ValidateSourceName, e.Source),
	}
	for _, tag := range e.Tags {
		errs = append(errs, ValidateTag(tag))
	}
	errs = append(errs, validateOptionalUTF8("url", e.URL), validateOptionalUTF8("author", e.Author))

	return join(errs...)
}

// Validate reports every way a breaks the domain rules, or nil.
func (a Annotation) Validate() error {
	return join(
		validateAnnotationKind(a.Kind),
		ValidateText("body", a.Body),
		validateOptional(ValidateSourceName, a.Source),
		validateOptionalUTF8("url", a.URL),
		validateOptionalUTF8("author", a.Author),
	)
}

// Validate reports every way s breaks the domain rules, or nil.
func (s Source) Validate() error {
	return validateSourceName("name", s.Name)
}

// CompareEvents orders events by occurred at, then timeline id, then seq, for
// [slices.SortFunc], so events of several timelines merge into one deterministic order. A
// coarse event sorts at the start of its window, so "10:15" precedes every 10:15:xx event.
func CompareEvents(a, b Event) int {
	return cmp.Or(a.OccurredAt.Compare(b.OccurredAt), cmp.Compare(a.TimelineID, b.TimelineID), cmp.Compare(a.Seq, b.Seq))
}

// ValidateText reports s, the value of field, when it is blank or not UTF-8, or nil.
func ValidateText(field, s string) error {
	return join(validateNotBlank(field, field, s), ValidateUTF8(field, s))
}

// ValidateUTF8 reports s, the value of field, when it is not UTF-8, which JSON cannot carry
// byte for byte, or nil.
func ValidateUTF8(field, s string) error {
	return validateUTF8(field, field, s)
}

// validateOptionalUTF8 is [ValidateUTF8] for a field that may be absent.
func validateOptionalUTF8(field string, s *string) error {
	if s == nil {
		return nil
	}

	return ValidateUTF8(field, *s)
}

func validateUTF8(field, what, s string) error {
	if !utf8.ValidString(s) {
		return problem(field, RuleNotUTF8, "%s: must be UTF-8", what)
	}

	return nil
}

func validateNotBlank(field, what, s string) error {
	// SQLite's length() stops at the first NUL, so the store's CHECK reads NUL-led text as empty.
	if trimmed := strings.TrimSpace(s); trimmed == "" || trimmed[0] == 0 {
		return problem(field, RuleBlank, "%s: must not be blank", what)
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
	if kind.Valid() {
		return nil
	}

	return problem("kind", RuleUnknown, "annotation kind %q: must be %q or %q", kind, AnnotationEvidence, AnnotationComment)
}

func validatePrecision(precision Precision) error {
	if precision.Valid() {
		return nil
	}

	names := make([]string, len(precisions()))
	for i, p := range precisions() {
		names[i] = string(p)
	}
	last := len(names) - 1

	return problem("precision", RuleUnknown, "precision %q: must be %s or %s", precision, strings.Join(names[:last], ", "), names[last])
}

func validateOccurredAt(at time.Time, offset *int, precision Precision) error {
	err := ValidateInstantRange("occurred_at", "occurred at", at)
	if err != nil || !precision.Valid() {
		return err
	}

	zone := time.UTC
	if offset != nil {
		zone = time.FixedZone("", *offset)
	}
	if !precision.Truncate(at.In(zone)).Equal(at) {
		return problem("occurred_at", RulePrecisionStart, "occurred at %s: must be the start of its %s", at.In(zone).Format(time.RFC3339Nano), precision)
	}

	return nil
}

// MaxOccurredOffset is the furthest an occurred offset may be from UTC, in seconds.
// It mirrors the event table's occurred_offset CHECK.
const MaxOccurredOffset = 86400

// ValidateOccurredOffset reports an offset more than [MaxOccurredOffset] from UTC, or nil.
func ValidateOccurredOffset(offset *int) error {
	if offset == nil || (*offset >= -MaxOccurredOffset && *offset <= MaxOccurredOffset) {
		return nil
	}

	// uint so negating math.MinInt cannot overflow
	sign, abs := '+', uint(*offset)
	if *offset < 0 {
		sign, abs = '-', -abs
	}
	text := fmt.Sprintf("%c%02d:%02d", sign, abs/3600, abs%3600/60)
	if abs%60 != 0 {
		text += fmt.Sprintf(":%02d", abs%60)
	}

	return problem("occurred_offset", RuleOffsetRange, "occurred offset %s: must be within a day of UTC", text)
}

// ValidateEndedAt reports an ended at the canonical instant cannot render, or one before occurred, or nil.
func ValidateEndedAt(ended *time.Time, occurred time.Time) error {
	if ended == nil {
		return nil
	}

	err := ValidateInstantRange("ended_at", "ended at", *ended)
	if err != nil {
		return err
	}
	if ended.Before(occurred) {
		return problem("ended_at", RuleEndedBeforeOccurred, "ended at %s: must not be before occurred at %s", FormatInstant(*ended), FormatInstant(occurred))
	}

	return nil
}

// ValidateInstantRange reports t, the value of field, when its UTC year is outside
// 0000–9999, which the canonical instant cannot render, or nil.
func ValidateInstantRange(field, what string, t time.Time) error {
	if year := t.UTC().Year(); year < 0 || year > 9999 {
		return problem(field, RuleYearRange, "%s: year %d is outside 0000–9999", what, year)
	}

	return nil
}
