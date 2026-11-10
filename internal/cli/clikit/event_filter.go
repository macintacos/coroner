package clikit

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddEventFilterFlags declares the flags that filter events, which mean the same on every
// command that takes them.
func AddEventFilterFlags(flags *pflag.FlagSet) {
	flags.String("since", "", "Keep events at or after this `time`")
	flags.String("until", "", "Keep events before this `time`")
	AddEventMatchFlags(flags)
	flags.String("order", "asc", "Order by occurred at, then timeline id, then seq, as an `order`: {asc | desc}")
	flags.Int("limit", 0, "Show at most this `number` of events (default: every one)")
}

// AddEventMatchFlags declares the flags that pick events by source, tag and text.
func AddEventMatchFlags(flags *pflag.FlagSet) {
	flags.StringArray("source", nil, "Keep events from this `source`; repeat to match any of several")
	flags.StringArray("tag", nil, "Keep events with this `tag`; repeat to require all of them")
	flags.String("contains", "", "Keep events whose text contains this `text`, ignoring case")
}

// ReadEventFilter returns the filter the flags ask for, and the times --since and --until
// resolved to as the command echoes them, or "" when neither was given. It rejects a filter
// before the store opens.
func ReadEventFilter(logger *slog.Logger, flags *pflag.FlagSet, now time.Time, z Zone) (store.EventFilter, string, error) {
	f, err := ReadEventMatch(flags)
	if err != nil {
		return f, "", err
	}
	f.Order, err = readOrder(flags)
	if err != nil {
		return f, "", err
	}
	f.Limit, err = readLimit(flags)
	if err != nil {
		return f, "", err
	}

	var since, until string
	f.Since, since, err = ReadBound(logger, flags, "since", now, z)
	if err != nil {
		return f, "", err
	}
	f.Until, until, err = ReadBound(logger, flags, "until", now, z)

	return f, strings.TrimSpace(since + " " + until), err
}

// ReadEventMatch returns the sources, tags and text the match flags keep events by, rejecting
// them before the store opens.
func ReadEventMatch(flags *pflag.FlagSet) (store.EventFilter, error) {
	f := store.EventFilter{
		Sources: AppendUnique(nil, stringArray(flags, "source")...),
		Tags:    AppendUnique(nil, stringArray(flags, "tag")...),
	}

	err := CheckFilterSlugs(f)
	if err != nil {
		return f, err
	}
	f.Contains, err = readContains(flags)

	return f, err
}

// CheckFilterSlugs rejects a filter whose sources or tags could name no stored one.
func CheckFilterSlugs(f store.EventFilter) error {
	for _, source := range f.Sources {
		err := CheckSource(source)
		if err != nil {
			return err
		}
	}
	for _, tag := range f.Tags {
		err := CheckTag(tag)
		if err != nil {
			return err
		}
	}

	return nil
}

func readContains(flags *pflag.FlagSet) (string, error) {
	contains := ChangedString(flags, "contains")
	if contains == nil {
		return "", nil
	}

	return *contains, checkNotBlank("--contains", *contains)
}

func readOrder(flags *pflag.FlagSet) (store.Order, error) {
	order, _ := flags.GetString("order")
	switch order {
	case "asc":
		return store.OrderAsc, nil
	case "desc":
		return store.OrderDesc, nil
	}

	return store.OrderAsc, &Error{
		Kind: KindUsage, Message: fmt.Sprintf("--order %q is not asc or desc", order), Hint: "Pass --order asc or --order desc.",
	}
}

func readLimit(flags *pflag.FlagSet) (int, error) {
	limit, _ := flags.GetInt("limit")
	if flags.Changed("limit") && limit < 1 {
		return 0, &Error{
			Kind: KindUsage, Message: fmt.Sprintf("--limit %d is below 1", limit), Hint: "Leave --limit out to list every event.",
		}
	}

	return limit, nil
}

// ReadBound resolves the time flag name gives, and how the command echoes it; the time is
// nil and the echo "" when the flag was not given.
func ReadBound(logger *slog.Logger, flags *pflag.FlagSet, name string, now time.Time, z Zone) (*time.Time, string, error) {
	input := ChangedString(flags, name)
	if input == nil {
		return nil, "", nil
	}

	r, err := ResolveTime("--"+name, input, now, z.Loc)
	if err != nil {
		return nil, "", err
	}
	LogTypedTime(logger, TypedTime{Flag: name, Input: *input, Zone: z, Resolved: r})

	return &r.Instant, name + " " + FormatResolved(r, z), nil
}

// NotifyTruncated says on stderr when page's limit left events out.
func NotifyTruncated(o Output, page store.EventPage) error {
	if !page.Truncated {
		return nil
	}

	return o.Notify("Showing %d of %d events; raise --limit, or leave it out, to see them all.", len(page.Events), page.Total)
}

// WithTimelineNames prepends to rows[i] the name of events[i]'s timeline; rows and events must
// be parallel.
func WithTimelineNames(rows [][]string, events []timeline.Event, timelines []timeline.Timeline) [][]string {
	names := make(map[string]string, len(timelines))
	for _, t := range timelines {
		names[t.ID] = t.Name
	}
	for i, e := range events {
		rows[i] = append([]string{names[e.TimelineID]}, rows[i]...)
	}

	return rows
}
