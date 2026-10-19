package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// selectedBy names the layer that selected the timeline a command acts on.
type selectedBy string

const (
	selectedByFlag selectedBy = "flag"
	selectedByEnv  selectedBy = "env"
	selectedByUse  selectedBy = "use"
)

// setting returns how a user sets this layer: -t, CORONER_TIMELINE or 'coroner timeline use'.
func (by selectedBy) setting() string {
	switch by {
	case selectedByFlag:
		return "-t"
	case selectedByEnv:
		return "CORONER_TIMELINE"
	case selectedByUse:
		return "'coroner timeline use'"
	}

	return string(by)
}

// addTimelineFlag declares -t/--timeline, so every command registers the same name,
// shorthand and usage.
// It is a StringArray, not a StringSlice, so a value is never split on commas.
func addTimelineFlag(flags *pflag.FlagSet) {
	flags.StringArrayP("timeline", "t", nil, timelineUsage)
}

const timelineUsage = "Timeline `name|id` to act on"

// timelineField is -t as a write command's field: a stringsField because -t is a StringArray
// on every command; wrapRunErrors refuses a second timeline.
func timelineField() field {
	return field{name: "timeline", shorthand: "t", typ: stringsField, usage: timelineUsage, check: withoutEnv(checkTimelineFlag)}
}

// timelinesAnnotation is the cmd.Annotations key listing, comma-separated, how a command names
// its timelines: several (a repeated -t, and --overlay where registered, overlay timelines),
// positional (a positional name selects the timeline) and guard (-t only, no environment or
// current timeline). A command without it acts on one timeline.
const timelinesAnnotation = "timelines"

// oneTimelineFormatsAnnotation is the cmd.Annotations key listing, comma-separated, the formats
// of a several-timeline command that print one timeline.
const oneTimelineFormatsAnnotation = "oneTimelineFormats"

// timelineRule is how many timelines a command reads, and how they may be named.
type timelineRule struct {
	command    string
	several    bool
	positional bool
	guard      bool
	oneFormats []format
}

// timelineInput is what the command line names, read and checked before the store opens.
type timelineInput struct {
	names      []string
	overlay    *string
	positional *string
	rule       timelineRule
}

// ruleOf returns cmd's timelineRule, read from its timelinesAnnotation and
// oneTimelineFormatsAnnotation.
func ruleOf(cmd *cobra.Command) timelineRule {
	tokens := strings.Split(cmd.Annotations[timelinesAnnotation], ",")
	rule := timelineRule{
		command:    cmd.CommandPath(),
		several:    slices.Contains(tokens, "several"),
		positional: slices.Contains(tokens, "positional"),
		guard:      slices.Contains(tokens, "guard"),
	}
	if formats := cmd.Annotations[oneTimelineFormatsAnnotation]; formats != "" {
		for f := range strings.SplitSeq(formats, ",") {
			rule.oneFormats = append(rule.oneFormats, format(f))
		}
	}

	return rule
}

// readTimelineInput returns the timelines flags and args name under rule, or usage when -t or
// --overlay is empty (A-9), --overlay is given with -t, a positional name is given with -t, or
// a one-timeline command is given several -t values. It does no I/O, so a timeline's name and
// its id are two values.
func readTimelineInput(flags *pflag.FlagSet, args []string, rule timelineRule) (timelineInput, error) {
	in := timelineInput{rule: rule}
	if flags.Lookup("timeline") == nil {
		return in, nil
	}

	var err error
	in.names, err = readTimelineNames(flags)
	if err != nil {
		return timelineInput{}, err
	}
	in.overlay, err = readOverlay(flags, in.names)
	if err != nil {
		return timelineInput{}, err
	}
	if rule.positional && len(args) == 1 {
		if len(in.names) > 0 {
			return timelineInput{}, &Error{
				Kind:    KindUsage,
				Message: "pass either a timeline name or id, or -t",
				Hint:    "Run 'coroner timeline view --help' for usage.",
			}
		}
		in.positional = &args[0]
	}
	if !rule.several && len(in.names) > 1 {
		return timelineInput{}, &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("-t is given %d different values, and %s acts on one timeline", len(in.names), rule.command),
			Hint: "Pass -t once. 'coroner event list', 'coroner timeline export', " +
				"'coroner timeline view' and 'coroner overlay create' read several timelines.",
		}
	}

	return in, nil
}

// readTimelineNames returns -t's values in the order given, each once, or usage when one is
// empty (A-9).
func readTimelineNames(flags *pflag.FlagSet) ([]string, error) {
	names := appendUnique(nil, stringArray(flags, "timeline")...)
	for _, name := range names {
		err := checkTimelineFlag(name)
		if err != nil {
			return nil, err
		}
	}

	return names, nil
}

// readOverlay returns --overlay, nil when it was not given, or usage when it is empty (A-9) or
// given with names, -t's values.
func readOverlay(flags *pflag.FlagSet, names []string) (*string, error) {
	overlay := changedString(flags, "overlay")
	switch {
	case overlay != nil && *overlay == "":
		return nil, &Error{Kind: KindUsage, Message: "--overlay must not be empty", Hint: "Pass --overlay with an overlay's name."}
	case overlay != nil && len(names) > 0:
		return nil, &Error{
			Kind: KindUsage, Message: "--overlay and -t both select timelines",
			Hint: "Pass --overlay <name>, or -t for each timeline, not both.",
		}
	}

	return overlay, nil
}

// timelineFlag returns -t's value, nil when it was not given, or usage when it is empty (A-9).
// Commands call it at the top of RunE, before any I/O. wrapRunErrors has already refused a
// second timeline, so the first value is the only one.
func timelineFlag(flags *pflag.FlagSet) (*string, error) {
	names, err := timelineFlags(flags)
	if err != nil || len(names) == 0 {
		return nil, err
	}

	return &names[0], nil
}

// timelineFlags returns -t's values in the order given, each once, or usage when one is empty
// (A-9).
func timelineFlags(flags *pflag.FlagSet) ([]string, error) {
	in, err := readTimelineInput(flags, nil, timelineRule{several: true})
	return in.names, err
}

func checkTimelineFlag(value string) error {
	if value != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: "-t must not be empty", Hint: "Pass -t with a timeline name or id."}
}

// resolveTimeline returns the nameOrID that selects the timeline, and its layer: flag when
// -t was given, then a non-empty CORONER_TIMELINE, then stored, the current timeline's id.
// It returns ("", "") when no layer selects one.
func resolveTimeline(flag *string, lookupEnv func(string) (string, bool), stored string) (string, selectedBy) {
	env, _ := lookupEnv("CORONER_TIMELINE")
	switch {
	case flag != nil:
		return *flag, selectedByFlag
	case env != "":
		return env, selectedByEnv
	case stored != "":
		return stored, selectedByUse
	}

	return "", ""
}

// lookupTimeline returns the timeline -t, CORONER_TIMELINE or the current timeline selects, and
// its layer; by is "" and err nil when no layer selects one. A layer naming a missing timeline
// is timeline_not_found rather than falling through to the next, so a typo never acts on
// another timeline.
func lookupTimeline(
	ctx context.Context, s *store.Store, flag *string, lookupEnv func(string) (string, bool),
) (timeline.Timeline, selectedBy, error) {
	stored, err := s.CurrentTimeline(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return stored, "", err
	}

	nameOrID, by := resolveTimeline(flag, lookupEnv, stored.ID)
	var layer string
	switch by {
	case selectedByUse:
		return stored, by, nil
	case selectedByFlag:
		layer = fmt.Sprintf("-t %q", nameOrID)
	case selectedByEnv:
		layer = fmt.Sprintf("CORONER_TIMELINE=%q", nameOrID)
	default:
		return timeline.Timeline{}, by, nil
	}

	t, err := findTimeline(ctx, s, nameOrID)
	notFound, ok := errors.AsType[*Error](err)
	if ok && notFound.Kind == KindTimelineNotFound {
		return t, by, &Error{
			Kind: KindTimelineNotFound, Message: layer + ": no such timeline", Hint: notFound.Hint, Err: notFound.Err,
		}
	}

	return t, by, err
}

// selectTimeline is lookupTimeline for commands that need a timeline: when no layer selects
// one it is timeline_required, with a hint listing the timelines.
func selectTimeline(
	ctx context.Context, s *store.Store, flag *string, lookupEnv func(string) (string, bool),
) (timeline.Timeline, error) {
	t, by, err := lookupTimeline(ctx, s, flag, lookupEnv)
	if err == nil && by == "" {
		return t, timelineRequiredError(ctx, s)
	}

	return t, err
}

// selectTimelines returns the timeline each of names, -t's values, selects, each once in the
// order named, or, when names is empty, the one timeline selectTimeline selects. A name no
// timeline has is timeline_not_found, naming it.
func selectTimelines(
	ctx context.Context, s *store.Store, names []string, lookupEnv func(string) (string, bool),
) ([]timeline.Timeline, error) {
	if len(names) == 0 {
		t, err := selectTimeline(ctx, s, nil, lookupEnv)
		return []timeline.Timeline{t}, err
	}

	var timelines []timeline.Timeline
	for _, name := range names {
		t, _, err := lookupTimeline(ctx, s, &name, lookupEnv)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(timelines, func(selected timeline.Timeline) bool { return selected.ID == t.ID }) {
			timelines = append(timelines, t)
		}
	}

	return timelines, nil
}

// timelineRequiredError returns timeline_required, with a hint naming the first few timelines
// there are.
func timelineRequiredError(ctx context.Context, s *store.Store) error {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return err
	}

	const shown = 10
	hint := "Pass -t <name>, set CORONER_TIMELINE, or run 'coroner timeline use <name>'."
	if len(timelines) > 0 {
		names := make([]string, 0, shown)
		for _, t := range timelines[:min(len(timelines), shown)] {
			names = append(names, t.Name)
		}
		if more := len(timelines) - len(names); more > 0 {
			names = append(names, fmt.Sprintf("and %d more", more))
		}
		hint = "Timelines: " + strings.Join(names, ", ") + "\n" + hint
	}

	return &Error{Kind: KindTimelineRequired, Message: "no timeline selected", Hint: hint}
}

// shadowNotice returns what the timeline picker says while CORONER_TIMELINE selects the
// timeline commands act on over the current timeline, or "" when the variable is unset or
// empty.
func shadowNotice(lookupEnv func(string) (string, bool)) string {
	nameOrID, by := resolveTimeline(nil, lookupEnv, "")
	if by != selectedByEnv {
		return ""
	}

	return fmt.Sprintf("CORONER_TIMELINE=%q selects the timeline commands act on", nameOrID)
}

// shadowWarning returns the warning printed once picked is stored while CORONER_TIMELINE
// selects another timeline, or "" when it is unset, empty or names picked.
func shadowWarning(lookupEnv func(string) (string, bool), picked timeline.Timeline) string {
	nameOrID, by := resolveTimeline(nil, lookupEnv, "")
	if by != selectedByEnv || nameOrID == picked.Name || nameOrID == picked.ID {
		return ""
	}

	return fmt.Sprintf("CORONER_TIMELINE=%q still selects the timeline commands act on. Unset it to act on `%s`.",
		nameOrID, picked.Name)
}

// addOverlayFlag declares --overlay, on a command whose -t reads several timelines.
func addOverlayFlag(flags *pflag.FlagSet) {
	flags.String("overlay", "", "Read the timelines of the overlay called `name`, instead of -t")
}

// timelineChoice is what selects a command's timelines: -t's names, or a saved overlay.
type timelineChoice struct {
	names   []string
	overlay *string
}

// readTimelineChoice returns the timelines -t or --overlay names, or usage when --overlay is
// empty (A-9) or given with -t. It runs before the store opens.
func readTimelineChoice(flags *pflag.FlagSet) (timelineChoice, error) {
	in, err := readTimelineInput(flags, nil, timelineRule{several: true})
	return timelineChoice{names: in.names, overlay: in.overlay}, err
}

// selectChosenTimelines returns the timelines choice selects: its overlay's, in the overlay's
// order, or those selectTimelines selects for its names.
func selectChosenTimelines(
	ctx context.Context, s *store.Store, choice timelineChoice, lookupEnv func(string) (string, bool),
) ([]timeline.Timeline, error) {
	if choice.overlay == nil {
		return selectTimelines(ctx, s, choice.names, lookupEnv)
	}

	o, err := s.OverlayByName(ctx, *choice.overlay)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, &Error{
			Kind: KindOverlayNotFound, Message: fmt.Sprintf("no overlay is named %q", *choice.overlay),
			Hint: "Save one with 'coroner overlay create <name> -t <timeline>...'.", Err: err,
		}
	case err != nil:
		return nil, err
	case len(o.TimelineIDs) == 0:
		return nil, &Error{
			Kind: KindTimelineNotFound, Message: fmt.Sprintf("overlay `%s` reads no timelines: each one was deleted", o.Name),
			Hint: "Pass -t for each timeline to read.",
		}
	}

	return timelinesByID(ctx, s, o.TimelineIDs)
}
