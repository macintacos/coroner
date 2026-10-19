package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
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

// storedLookup is what the planner reads of the database: every timeline and source, and records
// by id, with store.ErrNotFound when nothing has the id. A *store.Tx satisfies it.
type storedLookup interface {
	Timelines(ctx context.Context) ([]timeline.Timeline, error)
	Sources(ctx context.Context) ([]timeline.Source, error)
	Event(ctx context.Context, id string) (timeline.Event, error)
	Annotation(ctx context.Context, id string) (timeline.Annotation, error)
	Attachment(ctx context.Context, id string) (timeline.Attachment, error)
	Link(ctx context.Context, id string) (timeline.Link, error)
	LinkBetween(ctx context.Context, fromEventID, toEventID string, kind timeline.LinkKind) (timeline.Link, error)
}

// storedRecords is the in-memory storedLookup, for specs and a database that does not exist yet.
type storedRecords struct {
	timelines   []timeline.Timeline
	sources     []timeline.Source
	events      map[string]timeline.Event
	annotations map[string]timeline.Annotation
	attachments map[string]timeline.Attachment
	links       map[string]timeline.Link
	// linksByJoin are the links by linkJoin.
	linksByJoin map[string]timeline.Link
}

func (s storedRecords) Timelines(context.Context) ([]timeline.Timeline, error) {
	return s.timelines, nil
}

func (s storedRecords) Sources(context.Context) ([]timeline.Source, error) {
	return s.sources, nil
}

func (s storedRecords) Event(_ context.Context, id string) (timeline.Event, error) {
	return storedByID(s.events, id)
}

func (s storedRecords) Annotation(_ context.Context, id string) (timeline.Annotation, error) {
	return storedByID(s.annotations, id)
}

func (s storedRecords) Attachment(_ context.Context, id string) (timeline.Attachment, error) {
	return storedByID(s.attachments, id)
}

func (s storedRecords) Link(_ context.Context, id string) (timeline.Link, error) {
	return storedByID(s.links, id)
}

func (s storedRecords) LinkBetween(_ context.Context, fromEventID, toEventID string, kind timeline.LinkKind) (timeline.Link, error) {
	return storedByID(s.linksByJoin, linkJoin(fromEventID, toEventID, kind))
}

func storedByID[T any](records map[string]T, id string) (T, error) {
	r, ok := records[id]
	if !ok {
		return r, store.ErrNotFound
	}

	return r, nil
}

// lookUp returns the record get finds under id, and whether one is stored.
func lookUp[T any](ctx context.Context, id string, get func(context.Context, string) (T, error)) (T, bool, error) {
	r, err := get(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return r, false, nil
	case err != nil:
		return r, false, err
	}

	return r, true, nil
}

// importInput is everything planImport decides from.
type importInput struct {
	records []export.Record
	// sidecars are the attachment files found beside the input, by sha256.
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
	result importResultJSON
	// problems are why the input cannot be imported; when there are any, nothing else is planned.
	problems []string
	// takenName is an incoming timeline's name that a stored timeline with another id holds, or "".
	takenName string
	blocked   blockReason
	// unreplaceable are the conflicts --on-conflict replace cannot overwrite.
	unreplaceable []importConflict
	// identical are the stored events the input holds unchanged.
	identical []timeline.Event
	// timelineNames are the sorted names of the timelines the import writes to, or would.
	timelineNames []string
}

// blockReason is why a plan whose input is valid writes nothing.
type blockReason int

const (
	blockNone blockReason = iota
	// blockConflicts is a conflict under --on-conflict fail.
	blockConflicts
	// blockUnreplaceable is a conflict --on-conflict replace cannot overwrite.
	blockUnreplaceable
)

// canWrite reports whether the plan may be written: no problems, no taken name and no block.
func (p importPlan) canWrite() bool {
	return len(p.problems) == 0 && p.takenName == "" && p.blocked == blockNone
}

// importResultJSON is import's result object: every key always present.
type importResultJSON struct {
	DryRun             bool                `json:"dry_run"`
	OnConflict         conflictPolicy      `json:"on_conflict"`
	Timelines          importCounts        `json:"timelines"`
	Sources            importCounts        `json:"sources"`
	Events             importCounts        `json:"events"`
	Annotations        importCounts        `json:"annotations"`
	Attachments        importCounts        `json:"attachments"`
	Links              importCounts        `json:"links"`
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
	links       []export.Link
}

// planImport decides what importing in.records writes: new records, replacements under
// --on-conflict replace, and nothing for records stored unchanged. A plan holding only problems
// means the input cannot be imported, and one holding only a takenName means a timeline's name
// belongs to a stored timeline with another id. A conflict the policy does not resolve sets the
// plan's blocked. It reads the database only through stored, and returns a read's error at once.
// ponytail: one read per id, under merge's write lock; batch the reads if large imports hold
// other writers up.
func planImport(ctx context.Context, in importInput, stored storedLookup) (importPlan, error) {
	timelines, err := stored.Timelines(ctx)
	if err != nil {
		return importPlan{}, err
	}
	sources, err := stored.Sources(ctx)
	if err != nil {
		return importPlan{}, err
	}
	incoming, problems := collectRecords(in.records)
	refs, err := newRecordIndex(ctx, incoming, stored, timelines)
	if err != nil {
		return importPlan{}, err
	}
	problems = append(problems, checkRecords(incoming, refs, in)...)
	if len(problems) > 0 {
		return importPlan{problems: problems}, nil
	}
	if name := takenTimelineName(incoming.timelines, timelines); name != "" {
		return importPlan{takenName: name}, nil
	}

	p := planner{in: in, refs: refs, lookup: stored, storedTimelines: map[string]timeline.Timeline{}, sources: newSourceIndex(sources), timelineIDs: map[string]bool{}}
	for _, t := range timelines {
		p.storedTimelines[t.ID] = t
	}
	p.result = importResultJSON{OnConflict: in.onConflict, Conflicts: []importConflict{}, MissingAttachments: []missingAttachment{}}
	p.planTimelines(incoming.timelines)
	p.planSources(incoming.sources)
	uses := slices.Concat(p.planEvents(incoming.events), p.planAnnotations(incoming.annotations))
	err = p.planAttachments(ctx, incoming.attachments)
	if err != nil {
		return importPlan{}, err
	}
	err = p.planLinks(ctx, incoming.links)
	if err != nil {
		return importPlan{}, err
	}
	p.planUnrecordedSources(uses)
	err = p.assignSourceIDs()
	if err != nil {
		return importPlan{}, err
	}

	return importPlan{
		write: p.write, result: p.result, blocked: p.blocked(), unreplaceable: p.unreplaceable,
		identical: p.identical, timelineNames: p.timelineNames(),
	}, nil
}

// The export keys the planner and event add --id treat specially.
const (
	keySeq        = "seq"
	keyCreatedAt  = "created_at"
	keyUpdatedAt  = "updated_at"
	keyTimelineID = "timeline_id"
	keyEventID    = "event_id"
)

// collectRecords returns records by type without headers and repeats, with event tags sorted and
// timeline refs once each as the database stores them, and a problem for each id the input holds
// twice with different content, or each timeline name it gives two ids.
func collectRecords(records []export.Record) (incomingRecords, []string) {
	var incoming incomingRecords
	for _, r := range records {
		switch r := r.(type) {
		case export.Timeline:
			r.Refs = firstOccurrences(r.Refs)
			incoming.timelines = append(incoming.timelines, r)
		case export.Source:
			incoming.sources = append(incoming.sources, r)
		case export.Event:
			r.Tags = sortedUnique(r.Tags)
			incoming.events = append(incoming.events, r)
		case export.Annotation:
			incoming.annotations = append(incoming.annotations, r)
		case export.Attachment:
			incoming.attachments = append(incoming.attachments, r)
		case export.Link:
			incoming.links = append(incoming.links, r)
		}
	}

	eventsWithoutSeq := func(a, b export.Event) []string { return eventDiff(a.Domain(), b.Domain()) }
	var idRepeats, nameRepeats, eventRepeats, annotationRepeats, attachmentRepeats, linkRepeats, joinRepeats []string
	incoming.timelines, idRepeats = uniqueRecords("timeline", incoming.timelines, func(t export.Timeline) string { return t.ID }, export.DifferingKeys)
	incoming.timelines, nameRepeats = uniqueRecords("timeline", incoming.timelines, func(t export.Timeline) string { return t.Name }, export.DifferingKeys)
	// Two ids for one source name are expected: each database gives a source its own id.
	incoming.sources, _ = uniqueRecords("source", incoming.sources, func(s export.Source) string { return s.Name }, export.DifferingKeys)
	incoming.events, eventRepeats = uniqueRecords("event", incoming.events, func(e export.Event) string { return e.ID }, eventsWithoutSeq)
	incoming.annotations, annotationRepeats = uniqueRecords("annotation", incoming.annotations, func(a export.Annotation) string { return a.ID }, export.DifferingKeys)
	incoming.attachments, attachmentRepeats = uniqueRecords("attachment", incoming.attachments, func(a export.Attachment) string { return a.ID }, export.DifferingKeys)
	incoming.links, linkRepeats = uniqueRecords("link", incoming.links, func(l export.Link) string { return l.ID }, export.DifferingKeys)
	incoming.links, joinRepeats = uniqueRecords("link", incoming.links, func(l export.Link) string { return linkJoin(l.FromEventID, l.ToEventID, l.Kind) }, export.DifferingKeys)

	return incoming, slices.Concat(idRepeats, nameRepeats, eventRepeats, annotationRepeats, attachmentRepeats, linkRepeats, joinRepeats)
}

// linkJoin names a link's kind and events, which the database holds at most one link for.
func linkJoin(fromEventID, toEventID string, kind timeline.LinkKind) string {
	return fmt.Sprintf("%s from %s to %s", kind, fromEventID, toEventID)
}

// sortedUnique returns tags sorted, each once, without changing tags.
func sortedUnique(tags []string) []string {
	sorted := slices.Clone(tags)
	slices.Sort(sorted)

	return slices.Compact(sorted)
}

// eventDiff returns the export keys in which incoming differs from stored, comparing tags sorted
// and once each, and leaving out seq and every key in ignore.
func eventDiff(stored, incoming timeline.Event, ignore ...string) []string {
	compared := func(e timeline.Event) export.Event {
		e.Tags = sortedUnique(e.Tags)
		return export.NewEvent(e)
	}

	return slices.DeleteFunc(export.DifferingKeys(compared(stored), compared(incoming)), func(k string) bool {
		return k == keySeq || slices.Contains(ignore, k)
	})
}

// firstOccurrences returns refs with each ref once, where it first appears, without changing refs.
func firstOccurrences(refs []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(slices.Clone(refs), func(ref string) bool {
		repeat := seen[ref]
		seen[ref] = true
		return repeat
	})
}

// uniqueRecords returns records with each key once, keeping the first, and a problem naming typ
// and the key for each later record under that key that differ finds different.
func uniqueRecords[T any](typ string, records []T, key func(T) string, differ func(T, T) []string) ([]T, []string) {
	first := map[string]T{}
	unique := records[:0:0]
	var problems []string
	for _, r := range records {
		kept, seen := first[key(r)]
		if !seen {
			first[key(r)] = r
			unique = append(unique, r)
			continue
		}
		if keys := differ(kept, r); len(keys) > 0 {
			problems = append(problems, fmt.Sprintf("%s %s: appears twice with different %s", typ, key(r), strings.Join(keys, ", ")))
		}
	}

	return unique, problems
}

// checkRecords returns a problem for each record that breaks a domain rule or a bound the database
// holds it to, lies outside the -t timeline, names a timeline, event or owner neither the input
// nor the database holds, or has an attachment file that does not match it.
func checkRecords(incoming incomingRecords, refs recordIndex, in importInput) []string {
	var problems []string
	addProblems := func(typ, id string, errs ...error) {
		for _, err := range errs {
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s %s: %s", typ, id, strings.ReplaceAll(err.Error(), "\n", "; ")))
			}
		}
	}

	for _, t := range incoming.timelines {
		addProblems("timeline", t.ID, t.Domain().Validate(), checkGuard(t.ID, in.guard))
	}
	for _, e := range incoming.events {
		addProblems("event", e.ID, e.Domain().Validate(), checkEventBounds(e), refs.timelineKnown(e.TimelineID), checkGuard(e.TimelineID, in.guard))
	}
	for _, a := range incoming.annotations {
		addProblems("annotation", a.ID, a.Domain().Validate(), refs.timelineKnown(a.TimelineID), refs.eventInTimeline(a.EventID, a.TimelineID),
			checkGuard(a.TimelineID, in.guard))
	}
	for _, a := range incoming.attachments {
		ownerTimeline, err := refs.ownerTimeline(a)
		addProblems("attachment", a.ID, err, checkGuard(ownerTimeline, in.guard), checkSidecar(a, in.sidecars))
	}
	for _, l := range incoming.links {
		addProblems("link", l.ID, l.Domain().Validate(), refs.eventKnown(l.FromEventID), refs.eventKnown(l.ToEventID),
			checkGuard(refs.events[l.FromEventID], in.guard), checkGuard(refs.events[l.ToEventID], in.guard))
	}

	return problems
}

// checkEventBounds returns an error when e's seq is so large that its timeline's next seq could
// overflow.
func checkEventBounds(e export.Event) error {
	if e.Seq != nil && *e.Seq > math.MaxInt64/2 {
		return fmt.Errorf("seq %d is larger than %d", *e.Seq, int64(math.MaxInt64/2))
	}

	return nil
}

// recordIndex finds the timeline, event or annotation a record refers to, in the input or the
// database.
type recordIndex struct {
	timelines   map[string]bool
	events      map[string]string // id -> timeline id
	annotations map[string]string // id -> timeline id
	// storedEvents and storedAnnotations are the stored ones whose ids the input names.
	storedEvents      map[string]timeline.Event
	storedAnnotations map[string]timeline.Annotation
}

// newRecordIndex indexes incoming records, the stored timelines, and the stored events and
// annotations incoming names, which it reads from stored, taking a stored event's or annotation's
// timeline over the input's, since an import never moves a record.
func newRecordIndex(ctx context.Context, incoming incomingRecords, stored storedLookup, timelines []timeline.Timeline) (recordIndex, error) {
	idx := recordIndex{timelines: map[string]bool{}, events: map[string]string{}, annotations: map[string]string{}}
	for _, t := range incoming.timelines {
		idx.timelines[t.ID] = true
	}
	for _, e := range incoming.events {
		idx.events[e.ID] = e.TimelineID
	}
	for _, a := range incoming.annotations {
		idx.annotations[a.ID] = a.TimelineID
	}
	for _, t := range timelines {
		idx.timelines[t.ID] = true
	}

	eventIDs, annotationIDs := namedRecordIDs(incoming)
	var err error
	idx.storedEvents, err = readStored(ctx, eventIDs, stored.Event)
	if err != nil {
		return recordIndex{}, err
	}
	idx.storedAnnotations, err = readStored(ctx, annotationIDs, stored.Annotation)
	if err != nil {
		return recordIndex{}, err
	}
	for id, e := range idx.storedEvents {
		idx.events[id] = e.TimelineID
	}
	for id, a := range idx.storedAnnotations {
		idx.annotations[id] = a.TimelineID
	}

	return idx, nil
}

// namedRecordIDs returns the event and annotation ids incoming names, sorted and each once.
func namedRecordIDs(incoming incomingRecords) ([]string, []string) {
	var events, annotations []string
	for _, e := range incoming.events {
		events = append(events, e.ID)
	}
	for _, a := range incoming.annotations {
		annotations = append(annotations, a.ID)
		if a.EventID != nil {
			events = append(events, *a.EventID)
		}
	}
	for _, a := range incoming.attachments {
		if a.EventID != nil {
			events = append(events, *a.EventID)
		}
		if a.AnnotationID != nil {
			annotations = append(annotations, *a.AnnotationID)
		}
	}
	for _, l := range incoming.links {
		events = append(events, l.FromEventID, l.ToEventID)
	}
	slices.Sort(events)
	slices.Sort(annotations)

	return slices.Compact(events), slices.Compact(annotations)
}

// readStored returns the records get finds under ids, leaving out those not stored.
func readStored[T any](ctx context.Context, ids []string, get func(context.Context, string) (T, error)) (map[string]T, error) {
	found := map[string]T{}
	for _, id := range ids {
		r, ok, err := lookUp(ctx, id, get)
		if err != nil {
			return nil, err
		}
		if ok {
			found[id] = r
		}
	}

	return found, nil
}

func (idx recordIndex) timelineKnown(id string) error {
	if idx.timelines[id] {
		return nil
	}

	return fmt.Errorf("its timeline %s is neither in the import nor in the database", id)
}

func (idx recordIndex) eventKnown(id string) error {
	if _, ok := idx.events[id]; ok {
		return nil
	}

	return fmt.Errorf("its event %s is neither in the import nor in the database", id)
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

// checkGuard returns an error when -t selected a timeline other than the one with timelineID. An
// empty timelineID is a record whose timeline is unknown, which another problem names.
func checkGuard(timelineID string, guard *timeline.Timeline) error {
	if guard == nil || timelineID == "" || timelineID == guard.ID {
		return nil
	}

	return fmt.Errorf("it belongs to timeline %s, not %s, which -t selects", timelineID, guard.Name)
}

// checkSidecar returns an error when a's file beside the input does not hold the content its
// sha256 and size name.
func checkSidecar(a export.Attachment, sidecars map[string][]byte) error {
	content, ok := sidecars[a.SHA256]
	if !ok {
		return nil
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != a.SHA256 || int64(len(content)) != a.Size {
		return fmt.Errorf("%s does not match its sha256 and size", export.AttachmentPath(a))
	}

	return nil
}

// takenTimelineName returns the name of the first incoming timeline that a stored timeline with
// another id holds, or "".
func takenTimelineName(incoming []export.Timeline, stored []timeline.Timeline) string {
	for _, t := range incoming {
		for _, existing := range stored {
			if existing.Name == t.Name && existing.ID != t.ID {
				return t.Name
			}
		}
	}

	return ""
}

// planner accumulates an import's writes and result.
type planner struct {
	in     importInput
	refs   recordIndex
	lookup storedLookup
	// storedTimelines are every stored timeline, by id.
	storedTimelines map[string]timeline.Timeline
	write           store.ImportRecords
	result          importResultJSON
	// unreplaceable are the conflicts --on-conflict replace cannot overwrite.
	unreplaceable []importConflict
	// identical are the stored events the input holds unchanged.
	identical []timeline.Event
	sources   sourceIndex
	// timelineIDs are the timelines the import writes to.
	timelineIDs map[string]bool
}

// sourceUse is a source name a new or replaced event or annotation names, with when that record
// was created.
type sourceUse struct {
	name      *string
	createdAt export.Instant
}

// resolve counts a record stored with c.Fields differing, and reports whether to replace it with
// the import's, which only a replaceable conflict under --on-conflict replace does.
func (p *planner) resolve(counts *importCounts, c importConflict, replaceable bool) bool {
	if len(c.Fields) == 0 {
		counts.Identical++
		return false
	}

	counts.Conflicting++
	p.result.Conflicts = append(p.result.Conflicts, c)
	if p.in.onConflict != onConflictReplace {
		return false
	}
	if !replaceable {
		p.unreplaceable = append(p.unreplaceable, c)
		return false
	}

	return true
}

func (p *planner) planTimelines(timelines []export.Timeline) {
	for _, t := range timelines {
		stored, ok := p.storedTimelines[t.ID]
		switch {
		case !ok:
			p.timelineIDs[t.ID] = true
			p.result.Timelines.Inserted++
			p.write.Timelines = append(p.write.Timelines, t.Domain())
		case p.resolve(&p.result.Timelines, importConflict{Type: "timeline", ID: t.ID, Fields: export.DifferingKeys(export.NewTimeline(stored), t)}, true):
			p.timelineIDs[t.ID] = true
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

// planEvents returns the source names of the events it inserts or replaces.
func (p *planner) planEvents(events []export.Event) []sourceUse {
	var uses []sourceUse
	for _, e := range events {
		if stored, ok := p.refs.storedEvents[e.ID]; ok {
			fields := eventDiff(stored, e.Domain())
			if len(fields) == 0 {
				p.identical = append(p.identical, stored)
			}
			if !p.resolve(&p.result.Events, importConflict{Type: "event", ID: e.ID, Fields: fields}, !slices.Contains(fields, keyTimelineID)) {
				continue
			}
			p.write.EventReplacements = append(p.write.EventReplacements, e.Domain())
		} else {
			p.result.Events.Inserted++
			p.write.Events = append(p.write.Events, e.Domain())
		}
		p.timelineIDs[e.TimelineID] = true
		uses = append(uses, sourceUse{name: e.Source, createdAt: e.CreatedAt})
	}

	return uses
}

// planAnnotations returns the source names of the annotations it inserts or replaces.
func (p *planner) planAnnotations(annotations []export.Annotation) []sourceUse {
	var uses []sourceUse
	for _, a := range annotations {
		if stored, ok := p.refs.storedAnnotations[a.ID]; ok {
			fields := export.DifferingKeys(export.NewAnnotation(stored), a)
			replaceable := !slices.Contains(fields, keyTimelineID) && !slices.Contains(fields, keyEventID)
			if !p.resolve(&p.result.Annotations, importConflict{Type: "annotation", ID: a.ID, Fields: fields}, replaceable) {
				continue
			}
			p.write.AnnotationReplacements = append(p.write.AnnotationReplacements, a.Domain())
		} else {
			p.result.Annotations.Inserted++
			p.write.Annotations = append(p.write.Annotations, a.Domain())
		}
		p.timelineIDs[a.TimelineID] = true
		uses = append(uses, sourceUse{name: a.Source, createdAt: a.CreatedAt})
	}

	return uses
}

// planAttachments inserts each new attachment whose content is beside the input or already beside
// the database, and lists the rest as missing. Attachments never change, so a stored one with
// different content cannot be replaced.
func (p *planner) planAttachments(ctx context.Context, attachments []export.Attachment) error {
	for _, a := range attachments {
		stored, ok, err := lookUp(ctx, a.ID, p.lookup.Attachment)
		if err != nil {
			return err
		}
		if ok {
			p.resolve(&p.result.Attachments, importConflict{Type: "attachment", ID: a.ID, Fields: export.DifferingKeys(export.NewAttachment(stored), a)}, false)
			continue
		}

		_, hasSidecar := p.in.sidecars[a.SHA256]
		if !hasSidecar && !p.in.localBlobs[a.SHA256] {
			p.result.MissingAttachments = append(p.result.MissingAttachments, missingAttachment{ID: a.ID, SHA256: a.SHA256})
			continue
		}
		p.result.Attachments.Inserted++
		p.write.Attachments = append(p.write.Attachments, a.Domain())
		// checkRecords has already rejected an attachment whose owner is unknown.
		ownerTimeline, _ := p.refs.ownerTimeline(a)
		p.timelineIDs[ownerTimeline] = true
	}

	return nil
}

// planLinks inserts each new link. Links never change, so a stored one with different content,
// or with another id for the same events and kind, cannot be replaced.
func (p *planner) planLinks(ctx context.Context, links []export.Link) error {
	for _, l := range links {
		stored, ok, err := lookUp(ctx, l.ID, p.lookup.Link)
		if err == nil && !ok {
			stored, ok, err = lookUp(ctx, l.ID, func(ctx context.Context, _ string) (timeline.Link, error) {
				return p.lookup.LinkBetween(ctx, l.FromEventID, l.ToEventID, l.Kind)
			})
		}
		if err != nil {
			return err
		}
		if ok {
			p.resolve(&p.result.Links, importConflict{Type: "link", ID: l.ID, Fields: export.DifferingKeys(export.NewLink(stored), l)}, false)
			continue
		}

		p.result.Links.Inserted++
		p.write.Links = append(p.write.Links, l.Domain())
		// checkRecords has already rejected a link whose event is unknown.
		p.timelineIDs[p.refs.events[l.FromEventID]] = true
	}

	return nil
}

// planUnrecordedSources creates each source uses name that is neither stored nor being created,
// created when the first record naming it was.
func (p *planner) planUnrecordedSources(uses []sourceUse) {
	for _, use := range uses {
		if use.name != nil && !p.sources.has(*use.name) {
			p.result.Sources.Inserted++
			p.addSource(timeline.Source{Name: *use.name, CreatedAt: time.Time(use.createdAt)})
		}
	}
}

// assignSourceIDs gives every source being created without an id a new one.
func (p *planner) assignSourceIDs() error {
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

// blocked returns what stops the import, or blockNone when it may write.
func (p *planner) blocked() blockReason {
	switch {
	case len(p.result.Conflicts) > 0 && p.in.onConflict == onConflictFail:
		return blockConflicts
	case len(p.unreplaceable) > 0:
		return blockUnreplaceable
	}

	return blockNone
}

// timelineNames returns the sorted names the timelines the import writes to have once it has.
func (p *planner) timelineNames() []string {
	names := map[string]string{}
	for id, t := range p.storedTimelines {
		names[id] = t.Name
	}
	for _, t := range slices.Concat(p.write.Timelines, p.write.TimelineReplacements) {
		names[t.ID] = t.Name
	}
	written := make([]string, 0, len(p.timelineIDs))
	for id := range p.timelineIDs {
		written = append(written, names[id])
	}
	slices.Sort(written)

	return written
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
