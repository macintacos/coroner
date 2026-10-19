package export

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
	// Sources are the events' sources in order of first appearance, then "" when an event
	// has none.
	Sources []string
}

// slot returns the palette index source takes: its index in Sources, wrapped by
// len(sourcePalette()).
func (v view) slot(source string) int {
	return slices.Index(v.Sources, source) % len(sourcePalette())
}

// prepare returns env as a view, failing naming the first record that refers to a timeline,
// event or annotation env does not hold, or when env holds no timeline.
func prepare(env Envelope) (view, error) {
	if len(env.Timelines) == 0 {
		return view{}, errors.New("want a timeline, got none")
	}
	names, err := timelineNames(env)
	if err != nil {
		return view{}, err
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	onTimeline, byEvent, err := splitAnnotations(env.Annotations, events)
	if err != nil {
		return view{}, err
	}
	attachments, err := groupAttachments(env)
	if err != nil {
		return view{}, err
	}
	links, err := sortedLinks(env)
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
// and those on each event, and fails naming one whose event is not in events.
func splitAnnotations(annotations []Annotation, events []Event) ([]Annotation, map[string][]Annotation, error) {
	exported := map[string]bool{}
	for _, e := range events {
		exported[e.ID] = true
	}

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
