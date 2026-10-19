package export

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timetext"
)

// view is an export made ready to show: checked once, in display order, with its lookups.
type view struct {
	// Timelines are as exported.
	Timelines []Timeline
	// Overlay is whether the export holds more than one timeline.
	Overlay bool
	// Names are the timelines' names by id in an overlay, and empty for one timeline.
	Names map[string]string
	// Events are in compareEvents order.
	Events []Event
	// OnTimeline are the annotations on no event, in compareAnnotations order.
	OnTimeline []Annotation
	// ByEvent are the annotations on each event by its id, in compareAnnotations order.
	ByEvent map[string][]Annotation
	// Attachments are by the id of the event or annotation that keeps each, in
	// compareAttachments order.
	Attachments map[string][]Attachment
	// Links are in compareLinks order.
	Links []Link
	// Sources are the names of the events' sources, in order of first appearance, then ""
	// when an event has none; unlike Envelope.Sources, they leave out a source only an
	// annotation names.
	Sources []string
}

// slot returns the palette index of source, a name in Sources other than "": its index
// there, wrapped by len(sourcePalette()).
func (v view) slot(source string) int {
	return slices.Index(v.Sources, source) % len(sourcePalette())
}

// prepare returns env as a view, or fails naming a record that leaves env inconsistent. env
// is consistent when it holds a timeline, and holds the timeline of every event and
// annotation, the event of every annotation, the event or annotation of every attachment,
// and both events of every link.
func prepare(env Envelope) (view, error) {
	if len(env.Timelines) == 0 {
		return view{}, errors.New("want a timeline, got none")
	}
	names, err := timelineNames(env)
	if err != nil {
		return view{}, err
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	exported := map[string]bool{}
	for _, e := range events {
		exported[e.ID] = true
	}
	onTimeline, byEvent, err := splitAnnotations(env.Annotations, exported)
	if err != nil {
		return view{}, err
	}
	attachments, err := groupAttachments(env, exported)
	if err != nil {
		return view{}, err
	}
	links, err := sortedLinks(env.Links, exported)
	if err != nil {
		return view{}, err
	}

	overlay := len(env.Timelines) > 1
	if !overlay {
		names = map[string]string{}
	}

	var sources []string
	for _, g := range groupEvents(events, sourceOf) {
		sources = append(sources, g.key)
	}

	return view{
		Timelines: env.Timelines, Overlay: overlay, Names: names, Events: events,
		OnTimeline: onTimeline, ByEvent: byEvent, Attachments: attachments, Links: links,
		Sources: sources,
	}, nil
}

// timelineNames returns each exported timeline's name by id, and fails naming an event or
// annotation whose timeline env does not hold.
func timelineNames(env Envelope) (map[string]string, error) {
	names := map[string]string{}
	for _, t := range env.Timelines {
		names[t.ID] = t.Name
	}
	for _, e := range env.Events {
		if _, ok := names[e.TimelineID]; !ok {
			return nil, fmt.Errorf("event %s: its timeline %s is not in the export", e.ID, e.TimelineID)
		}
	}
	for _, a := range env.Annotations {
		if _, ok := names[a.TimelineID]; !ok {
			return nil, fmt.Errorf("annotation %s: its timeline %s is not in the export", a.ID, a.TimelineID)
		}
	}

	return names, nil
}

// splitAnnotations returns annotations by created at then id, split into those on no event
// and those on each event, and fails naming one whose event is not exported.
func splitAnnotations(annotations []Annotation, exported map[string]bool) ([]Annotation, map[string][]Annotation, error) {
	var onTimeline []Annotation
	byEvent := map[string][]Annotation{}
	for _, a := range slices.SortedFunc(slices.Values(annotations), compareAnnotations) {
		switch {
		case a.EventID == nil:
			onTimeline = append(onTimeline, a)
		case exported[*a.EventID]:
			byEvent[*a.EventID] = append(byEvent[*a.EventID], a)
		default:
			return nil, nil, fmt.Errorf("annotation %s: its event %s is not in the export", a.ID, *a.EventID)
		}
	}

	return onTimeline, byEvent, nil
}

// groupAttachments returns env's attachments by created at then id, keyed by the id of the
// event or annotation that keeps each, and fails naming the first whose owner is not in env or
// whose event is not exported.
func groupAttachments(env Envelope, events map[string]bool) (map[string][]Attachment, error) {
	annotations := map[string]bool{}
	for _, a := range env.Annotations {
		annotations[a.ID] = true
	}

	byOwner := map[string][]Attachment{}
	for _, f := range slices.SortedFunc(slices.Values(env.Attachments), compareAttachments) {
		err := f.Domain().Owner.Validate()
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", f.ID, err)
		}
		switch {
		case f.EventID != nil && !events[*f.EventID]:
			return nil, fmt.Errorf("attachment %s: its event %s is not in the export", f.ID, *f.EventID)
		case f.AnnotationID != nil && !annotations[*f.AnnotationID]:
			return nil, fmt.Errorf("attachment %s: its annotation %s is not in the export", f.ID, *f.AnnotationID)
		}
		ownerID := deref(cmp.Or(f.EventID, f.AnnotationID))
		byOwner[ownerID] = append(byOwner[ownerID], f)
	}

	return byOwner, nil
}

// sortedLinks returns links by created at then id, and fails naming the first whose event is not
// exported.
func sortedLinks(links []Link, exported map[string]bool) ([]Link, error) {
	links = slices.SortedFunc(slices.Values(links), compareLinks)
	for _, l := range links {
		for _, id := range []string{l.FromEventID, l.ToEventID} {
			if !exported[id] {
				return nil, fmt.Errorf("link %s: its event %s is not in the export", l.ID, id)
			}
		}
	}

	return links, nil
}

// title returns one timeline's title and name, or "" and an overlay's names joined by " + ".
func (v view) title() (string, string) {
	if !v.Overlay {
		return v.Timelines[0].Title, v.Timelines[0].Name
	}
	names := make([]string, 0, len(v.Timelines))
	for _, t := range v.Timelines {
		names = append(names, t.Name)
	}

	return "", strings.Join(names, " + ")
}

// eventGroup is the events that share a key, such as a source; the group whose events have
// none has the key "".
type eventGroup struct {
	key    string
	events []Event
}

// groupEvents returns events under each key keys gives them, in order of first appearance,
// then the events keys gives none.
func groupEvents(events []Event, keys func(Event) []string) []eventGroup {
	var groups []eventGroup
	var keyless []Event
	index := map[string]int{}
	for _, e := range events {
		if len(keys(e)) == 0 {
			keyless = append(keyless, e)
		}
		for _, key := range keys(e) {
			i, ok := index[key]
			if !ok {
				i = len(groups)
				index[key] = i
				groups = append(groups, eventGroup{key: key})
			}
			groups[i].events = append(groups[i].events, e)
		}
	}
	if len(keyless) > 0 {
		groups = append(groups, eventGroup{events: keyless})
	}

	return groups
}

func sourceOf(e Event) []string {
	if e.Source == nil {
		return nil
	}

	return []string{*e.Source}
}

// lines returns s split at its line breaks, of any convention, without trailing blank lines.
func lines(s string) []string {
	split := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s), "\n")
	last := len(split) - 1
	for last > 0 && strings.TrimSpace(split[last]) == "" {
		last--
	}

	return split[:last+1]
}

func eventInstants(e Event) []time.Time {
	if e.EndedAt == nil {
		return []time.Time{time.Time(e.OccurredAt)}
	}

	return []time.Time{time.Time(e.OccurredAt), time.Time(*e.EndedAt)}
}

// readings returns each event's time as it reads in zone, by event id.
func readings(events []Event, zone *time.Location) map[string]timetext.Reading {
	byID := make(map[string]timetext.Reading, len(events))
	for _, e := range events {
		byID[e.ID] = timetext.Read(e.Domain(), zone)
	}

	return byID
}

func zoneName(t time.Time) string {
	name, _ := t.Zone()
	return name
}
