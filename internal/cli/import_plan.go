package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// shownImportProblems is how many problems an invalid_input error lists.
const shownImportProblems = 5

// conflictPolicy is what an import does with a record stored with different content.
type conflictPolicy string

const (
	onConflictFail    conflictPolicy = "fail"
	onConflictSkip    conflictPolicy = "skip"
	onConflictReplace conflictPolicy = "replace"
)

// storedRecords is what the database holds of what an import touches.
type storedRecords struct {
	// timelines and sources are every stored one.
	timelines map[string]timeline.Timeline
	sources   []timeline.Source
	// events, annotations and attachments are the stored ones whose ids the input names.
	events      map[string]timeline.Event
	annotations map[string]timeline.Annotation
	attachments map[string]timeline.Attachment
}

// importInput is everything planImport decides from.
type importInput struct {
	records []export.Record
	stored  storedRecords
	// sidecars are the attachment files found beside the input, by attachment id.
	sidecars map[string][]byte
	// localBlobs are the sha256s whose blobs are beside the database.
	localBlobs map[string]bool
	onConflict conflictPolicy
	// guard is the timeline -t selects, which every record must belong to; nil without -t.
	guard *timeline.Timeline
	newID func() (uuid.UUID, error)
	now   time.Time
}

// importPlan is what an import writes and reports.
type importPlan struct {
	write  store.ImportRecords
	result importResult
	// blocked is the id_conflict that stops the import from writing anything, or nil.
	blocked *Error
}

// importResult is import's result object: every key always present.
type importResult struct {
	DryRun             bool                `json:"dry_run"`
	OnConflict         conflictPolicy      `json:"on_conflict"`
	Timelines          importCounts        `json:"timelines"`
	Sources            importCounts        `json:"sources"`
	Events             importCounts        `json:"events"`
	Annotations        importCounts        `json:"annotations"`
	Attachments        importCounts        `json:"attachments"`
	Conflicts          []importConflict    `json:"conflicts"`
	MissingAttachments []missingAttachment `json:"missing_attachments"`
}

// importCounts sorts one record type's records: new, already stored unchanged, or stored with
// different content.
type importCounts struct {
	Inserted    int `json:"inserted"`
	Identical   int `json:"identical"`
	Conflicting int `json:"conflicting"`
}

type importConflict struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	// Fields are the export keys whose values differ.
	Fields []string `json:"fields"`
}

type missingAttachment struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// incomingRecords are the input's records by type, each id once and each source name once.
type incomingRecords struct {
	timelines   []export.Timeline
	sources     []export.Source
	events      []export.Event
	annotations []export.Annotation
	attachments []export.Attachment
}

// planImport decides what importing in.records writes: new records, replacements under
// --on-conflict replace, and nothing for records stored unchanged. It returns invalid_input when
// the input cannot be imported, and timeline_exists when a timeline's name belongs to a stored
// timeline with another id. A conflict the policy does not resolve sets the plan's blocked.
func planImport(in importInput) (importPlan, error) {
	incoming, problems := collectRecords(in.records)
	problems = append(problems, checkRecords(incoming, in)...)
	if len(problems) > 0 {
		return importPlan{}, invalidImportError(problems)
	}
	err := checkTimelineNames(incoming.timelines, in.stored.timelines)
	if err != nil {
		return importPlan{}, err
	}

	p := planner{in: in, sources: newSourceIndex(in.stored.sources)}
	p.result = importResult{OnConflict: in.onConflict, Conflicts: []importConflict{}, MissingAttachments: []missingAttachment{}}
	p.planTimelines(incoming.timelines)
	p.planSources(incoming.sources)
	p.planEvents(incoming.events)
	p.planAnnotations(incoming.annotations)
	p.planAttachments(incoming.attachments)
	err = p.planUnrecordedSources()
	if err != nil {
		return importPlan{}, err
	}

	return importPlan{write: p.write, result: p.result, blocked: p.blocked()}, nil
}

// collectRecords returns records by type without headers and repeats, and a problem for each id
// the input holds twice with different content, or each timeline name it gives two ids.
func collectRecords(records []export.Record) (incomingRecords, []string) {
	var in incomingRecords
	for _, r := range records {
		switch r := r.(type) {
		case export.Timeline:
			in.timelines = append(in.timelines, r)
		case export.Source:
			in.sources = append(in.sources, r)
		case export.Event:
			in.events = append(in.events, r)
		case export.Annotation:
			in.annotations = append(in.annotations, r)
		case export.Attachment:
			in.attachments = append(in.attachments, r)
		}
	}

	var problems []string
	in.timelines = uniqueRecords("timeline", in.timelines, func(t export.Timeline) string { return t.ID }, &problems)
	in.timelines = uniqueRecords("timeline", in.timelines, func(t export.Timeline) string { return t.Name }, &problems)
	in.sources = uniqueRecords("", in.sources, func(s export.Source) string { return s.Name }, nil)
	in.events = uniqueRecords("event", in.events, func(e export.Event) string { return e.ID }, &problems)
	in.annotations = uniqueRecords("annotation", in.annotations, func(a export.Annotation) string { return a.ID }, &problems)
	in.attachments = uniqueRecords("attachment", in.attachments, func(a export.Attachment) string { return a.ID }, &problems)

	return in, problems
}

// uniqueRecords returns records with each key once, keeping the first. When problems is not nil,
// a later record under a key with different content adds a problem naming typ and the key.
func uniqueRecords[T any](typ string, records []T, key func(T) string, problems *[]string) []T {
	first := map[string]T{}
	unique := records[:0:0]
	for _, r := range records {
		kept, seen := first[key(r)]
		switch {
		case !seen:
			first[key(r)] = r
			unique = append(unique, r)
		case problems != nil && len(differingKeys(kept, r)) > 0:
			*problems = append(*problems, fmt.Sprintf("%s %s: appears twice with different %s", typ, key(r), strings.Join(differingKeys(kept, r), ", ")))
		}
	}

	return unique
}

// checkRecords returns a problem for each record that breaks a domain rule, lies outside the -t
// timeline, names a timeline, event or owner neither the input nor the database holds, or has an
// attachment file that does not match it.
func checkRecords(in incomingRecords, input importInput) []string {
	refs := newRecordIndex(in, input.stored)
	var problems []string
	add := func(typ, id string, errs ...error) {
		for _, err := range errs {
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %s", typ, id, strings.ReplaceAll(err.Error(), "\n", "; ")))
			}
		}
	}

	for _, t := range in.timelines {
		add("timeline", t.ID, t.Domain().Validate(), refs.inGuard(t.ID, input.guard))
	}
	for _, e := range in.events {
		add("event", e.ID, e.Domain().Validate(), refs.timelineKnown(e.TimelineID), refs.inGuard(e.TimelineID, input.guard))
	}
	for _, a := range in.annotations {
		add("annotation", a.ID, a.Domain().Validate(), refs.timelineKnown(a.TimelineID), refs.eventInTimeline(a.EventID, a.TimelineID),
			refs.inGuard(a.TimelineID, input.guard))
	}
	for _, a := range in.attachments {
		ownerTimeline, err := refs.ownerTimeline(a)
		add("attachment", a.ID, err, refs.inGuard(ownerTimeline, input.guard), checkSidecar(a, input.sidecars))
	}

	return problems
}

// recordIndex finds the timeline, event or annotation a record refers to, in the input first and
// then in the database.
type recordIndex struct {
	timelines   map[string]bool
	events      map[string]string // id -> timeline id
	annotations map[string]string // id -> timeline id
}

func newRecordIndex(in incomingRecords, stored storedRecords) recordIndex {
	idx := recordIndex{timelines: map[string]bool{}, events: map[string]string{}, annotations: map[string]string{}}
	for id := range stored.timelines {
		idx.timelines[id] = true
	}
	for id, e := range stored.events {
		idx.events[id] = e.TimelineID
	}
	for id, a := range stored.annotations {
		idx.annotations[id] = a.TimelineID
	}
	for _, t := range in.timelines {
		idx.timelines[t.ID] = true
	}
	for _, e := range in.events {
		idx.events[e.ID] = e.TimelineID
	}
	for _, a := range in.annotations {
		idx.annotations[a.ID] = a.TimelineID
	}

	return idx
}

func (idx recordIndex) timelineKnown(id string) error {
	if idx.timelines[id] {
		return nil
	}

	return fmt.Errorf("its timeline %s is neither in the import nor in the database", id)
}

func (idx recordIndex) eventInTimeline(eventID *string, timelineID string) error {
	if eventID == nil {
		return nil
	}
	eventTimeline, ok := idx.events[*eventID]
	switch {
	case !ok:
		return fmt.Errorf("its event %s is neither in the import nor in the database", *eventID)
	case eventTimeline != timelineID:
		return fmt.Errorf("its event %s is in timeline %s, not %s", *eventID, eventTimeline, timelineID)
	}

	return nil
}

// ownerTimeline returns the id of the timeline a's event or annotation is in.
func (idx recordIndex) ownerTimeline(a export.Attachment) (string, error) {
	owners, kind, ownerID := idx.events, "event", a.EventID
	if ownerID == nil {
		owners, kind, ownerID = idx.annotations, "annotation", a.AnnotationID
	}
	timelineID, ok := owners[*ownerID]
	if !ok {
		return "", fmt.Errorf("its %s %s is neither in the import nor in the database", kind, *ownerID)
	}

	return timelineID, nil
}

// inGuard returns an error when -t selected a timeline other than the one with timelineID. An
// empty timelineID is a record whose timeline is unknown, which another problem names.
func (recordIndex) inGuard(timelineID string, guard *timeline.Timeline) error {
	if guard == nil || timelineID == "" || timelineID == guard.ID {
		return nil
	}

	return fmt.Errorf("it belongs to timeline %s, not %s, which -t selects", timelineID, guard.Name)
}

// checkSidecar returns an error when a's file beside the input does not hold the content its
// sha256 and size name.
func checkSidecar(a export.Attachment, sidecars map[string][]byte) error {
	content, ok := sidecars[a.ID]
	if !ok {
		return nil
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != a.SHA256 || int64(len(content)) != a.Size {
		return fmt.Errorf("%s does not match its sha256 and size", export.AttachmentPath(a))
	}

	return nil
}

// invalidImportError returns problems as one invalid_input error: a count, the first few, and
// how many more there are.
func invalidImportError(problems []string) *Error {
	header := fmt.Sprintf("%d records cannot be imported:", len(problems))
	if len(problems) == 1 {
		header = "1 record cannot be imported:"
	}
	lines := append([]string{header}, problems[:min(len(problems), shownImportProblems)]...)
	if more := len(problems) - shownImportProblems; more > 0 {
		lines = append(lines, fmt.Sprintf("and %d more", more))
	}

	return &Error{Kind: KindInvalidInput, Message: strings.Join(lines, "\n"), Hint: invalidImportHint}
}

const invalidImportHint = "Nothing was imported. Fix those records, or export the file again."

// checkTimelineNames returns timeline_exists for the first incoming timeline whose name a stored
// timeline with another id holds.
func checkTimelineNames(incoming []export.Timeline, stored map[string]timeline.Timeline) error {
	for _, t := range incoming {
		for _, s := range stored {
			if s.Name == t.Name && s.ID != t.ID {
				return &Error{
					Kind:    KindTimelineExists,
					Message: fmt.Sprintf("timeline `%s` already exists here as another timeline, so nothing was imported", t.Name),
					Hint:    fmt.Sprintf("Rename it with 'coroner timeline edit %s --name <new-name>', then import again.", t.Name),
				}
			}
		}
	}

	return nil
}

// planner accumulates an import's writes and result.
type planner struct {
	in     importInput
	write  store.ImportRecords
	result importResult
	// unreplaceable are the conflicts --on-conflict replace cannot overwrite.
	unreplaceable []importConflict
	sources       sourceIndex
	// sourceUses are the source names new and replaced events and annotations name, with when
	// the first of them was created.
	sourceUses []sourceUse
}

type sourceUse struct {
	name      *string
	createdAt export.Instant
}

// resolve counts a record stored with the keys fields differing, and reports whether to replace
// it. fixed are the keys a replacement cannot change.
func (p *planner) resolve(counts *importCounts, typ, id string, fields, fixed []string) bool {
	if len(fields) == 0 {
		counts.Identical++
		return false
	}

	counts.Conflicting++
	conflict := importConflict{Type: typ, ID: id, Fields: fields}
	p.result.Conflicts = append(p.result.Conflicts, conflict)
	if p.in.onConflict != onConflictReplace {
		return false
	}
	if slices.ContainsFunc(fields, func(f string) bool { return slices.Contains(fixed, f) }) {
		p.unreplaceable = append(p.unreplaceable, conflict)
		return false
	}

	return true
}

func (p *planner) planTimelines(timelines []export.Timeline) {
	for _, t := range timelines {
		stored, ok := p.in.stored.timelines[t.ID]
		switch {
		case !ok:
			p.result.Timelines.Inserted++
			p.write.Timelines = append(p.write.Timelines, t.Domain())
		case p.resolve(&p.result.Timelines, "timeline", t.ID, differingKeys(export.NewTimeline(stored), t), nil):
			replacement := t.Domain()
			replacement.UpdatedAt = p.in.now
			p.write.TimelineReplacements = append(p.write.TimelineReplacements, replacement)
		}
	}
}

func (p *planner) planSources(sources []export.Source) {
	for _, s := range sources {
		if p.sources.has(s.Name) {
			p.result.Sources.Identical++
			continue
		}
		source := s.Domain()
		if p.sources.ids[source.ID] {
			source.ID = ""
		}
		p.result.Sources.Inserted++
		p.addSource(source)
	}
}

func (p *planner) planEvents(events []export.Event) {
	for _, e := range events {
		stored, ok := p.in.stored.events[e.ID]
		switch {
		case !ok:
			p.result.Events.Inserted++
			p.write.Events = append(p.write.Events, e.Domain())
		case p.resolve(&p.result.Events, "event", e.ID, withoutKey(differingKeys(export.NewEvent(stored), e), "seq"), []string{"timeline_id"}):
			p.write.EventReplacements = append(p.write.EventReplacements, e.Domain())
		default:
			continue
		}
		p.sourceUses = append(p.sourceUses, sourceUse{name: e.Source, createdAt: e.CreatedAt})
	}
}

func (p *planner) planAnnotations(annotations []export.Annotation) {
	for _, a := range annotations {
		stored, ok := p.in.stored.annotations[a.ID]
		switch {
		case !ok:
			p.result.Annotations.Inserted++
			p.write.Annotations = append(p.write.Annotations, a.Domain())
		case p.resolve(&p.result.Annotations, "annotation", a.ID, differingKeys(export.NewAnnotation(stored), a), []string{"timeline_id", "event_id"}):
			p.write.AnnotationReplacements = append(p.write.AnnotationReplacements, a.Domain())
		default:
			continue
		}
		p.sourceUses = append(p.sourceUses, sourceUse{name: a.Source, createdAt: a.CreatedAt})
	}
}

// planAttachments inserts each new attachment whose content is beside the input or already beside
// the database, and lists the rest as missing. Attachments never change, so a stored one with
// different content cannot be replaced.
func (p *planner) planAttachments(attachments []export.Attachment) {
	for _, a := range attachments {
		if stored, ok := p.in.stored.attachments[a.ID]; ok {
			fields := differingKeys(export.NewAttachment(stored), a)
			p.resolve(&p.result.Attachments, "attachment", a.ID, fields, fields)
			continue
		}

		imported := store.ImportAttachment{Attachment: a.Domain()}
		content, hasSidecar := p.in.sidecars[a.ID]
		switch {
		case hasSidecar:
			imported.Content = bytes.NewReader(content)
		case !p.in.localBlobs[a.SHA256]:
			p.result.MissingAttachments = append(p.result.MissingAttachments, missingAttachment{ID: a.ID, SHA256: a.SHA256})
			continue
		}
		p.result.Attachments.Inserted++
		p.write.Attachments = append(p.write.Attachments, imported)
	}
}

// planUnrecordedSources creates each source a written event or annotation names that is neither
// stored nor in the input, created when the first record naming it was, and gives every source
// being created without an id a new one.
func (p *planner) planUnrecordedSources() error {
	for _, use := range p.sourceUses {
		if use.name != nil && !p.sources.has(*use.name) {
			p.result.Sources.Inserted++
			p.addSource(timeline.Source{Name: *use.name, CreatedAt: time.Time(use.createdAt)})
		}
	}

	for i, s := range p.write.Sources {
		if s.ID != "" {
			continue
		}
		id, err := p.in.newID()
		if err != nil {
			return err
		}
		p.write.Sources[i].ID = id.String()
	}

	return nil
}

func (p *planner) addSource(s timeline.Source) {
	p.write.Sources = append(p.write.Sources, s)
	p.sources.add(s)
}

// blocked returns the id_conflict that stops the import, or nil when it may write.
func (p *planner) blocked() *Error {
	switch {
	case len(p.result.Conflicts) > 0 && p.in.onConflict == onConflictFail:
		return &Error{
			Kind:    KindIDConflict,
			Message: recordCount(len(p.result.Conflicts)) + " conflict with what is stored, so nothing was imported",
			Hint:    "Pass --on-conflict skip to keep what is stored, or --on-conflict replace to take the import's.",
		}
	case len(p.unreplaceable) > 0:
		return &Error{
			Kind: KindIDConflict,
			Message: recordCount(len(p.unreplaceable)) +
				" cannot be replaced, so nothing was imported: an attachment never changes, and an event or annotation never moves",
			Hint: "Pass --on-conflict skip to keep what is stored.",
		}
	}

	return nil
}

func recordCount(n int) string {
	if n == 1 {
		return "1 record"
	}

	return fmt.Sprintf("%d records", n)
}

// sourceIndex is the source names and ids that exist once the import has written.
type sourceIndex struct {
	names map[string]bool
	ids   map[string]bool
}

func newSourceIndex(stored []timeline.Source) sourceIndex {
	idx := sourceIndex{names: map[string]bool{}, ids: map[string]bool{}}
	for _, s := range stored {
		idx.add(s)
	}

	return idx
}

func (idx sourceIndex) has(name string) bool {
	return idx.names[name]
}

func (idx sourceIndex) add(s timeline.Source) {
	idx.names[s.Name] = true
	if s.ID != "" {
		idx.ids[s.ID] = true
	}
}

// differingKeys returns the JSON keys of struct type T whose values encode differently in a and
// b, in declaration order.
func differingKeys[T any](a, b T) []string {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	var keys []string
	for field := range reflect.TypeFor[T]().Fields() {
		ja, errA := json.Marshal(va.FieldByIndex(field.Index).Interface())
		jb, errB := json.Marshal(vb.FieldByIndex(field.Index).Interface())
		if errA != nil || errB != nil || !bytes.Equal(ja, jb) {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			keys = append(keys, name)
		}
	}

	return keys
}

// withoutKey returns keys without key.
func withoutKey(keys []string, key string) []string {
	return slices.DeleteFunc(keys, func(k string) bool { return k == key })
}
