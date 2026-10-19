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
	selectedByFlag    selectedBy = "flag"
	selectedByEnv     selectedBy = "env"
	selectedByUse     selectedBy = "use"
	selectedByName    selectedBy = "name"
	selectedByOverlay selectedBy = "overlay"
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
	case selectedByName:
		return "the timeline argument"
	case selectedByOverlay:
		return "--overlay"
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

// selection is what selected a command's timelines: none, and by "", when nothing does.
type selection struct {
	timelines  []timeline.Timeline
	by         selectedBy
	oneFormats []format
}

// selectTimelines returns the timelines in selects: --overlay's, in the overlay's order, then the
// positional name, then each of -t's names once in the order named, then, unless in's rule is
// guard, a non-empty CORONER_TIMELINE and the current timeline. A layer naming a missing
// timeline is timeline_not_found rather than falling through to the next, so a typo never acts
// on another timeline.
func selectTimelines(
	ctx context.Context, s *store.Store, in timelineInput, lookupEnv func(string) (string, bool),
) (selection, error) {
	sel := selection{oneFormats: in.rule.oneFormats}
	var err error
	switch {
	case in.overlay != nil:
		sel.timelines, err = overlayTimelines(ctx, s, *in.overlay)
		sel.by = selectedByOverlay
	case in.positional != nil:
		var t timeline.Timeline
		t, err = findTimeline(ctx, s, *in.positional)
		sel.timelines, sel.by = []timeline.Timeline{t}, selectedByName
	case len(in.names) > 0:
		sel.timelines, err = namedTimelines(ctx, s, in.names)
		sel.by = selectedByFlag
	case !in.rule.guard:
		sel.timelines, sel.by, err = defaultTimeline(ctx, s, lookupEnv)
	}
	if err != nil {
		return selection{}, err
	}

	return sel, nil
}

// one returns the timeline sel selected, or timeline_required when it selected none.
func (sel selection) one(ctx context.Context, s *store.Store) (timeline.Timeline, error) {
	if len(sel.timelines) == 0 {
		return timeline.Timeline{}, timelineRequiredError(ctx, s)
	}

	return sel.timelines[0], nil
}

// all returns the timelines sel selected, or timeline_required when it selected none.
func (sel selection) all(ctx context.Context, s *store.Store) ([]timeline.Timeline, error) {
	if len(sel.timelines) == 0 {
		return nil, timelineRequiredError(ctx, s)
	}

	return sel.timelines, nil
}

// checkFormat returns usage when sel holds several timelines and f is one of the formats that
// print one. It runs after the lookup, so a timeline's name and its id count once, and before
// any read.
// ponytail: worded by format, since view's one-timeline format (json) and export's (svg) are disjoint; key by command if they ever overlap.
func (sel selection) checkFormat(f format) error {
	if len(sel.timelines) <= 1 || !slices.Contains(sel.oneFormats, f) {
		return nil
	}
	if f == formatJSON {
		return &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("--format json prints one timeline, and -t selects %d", len(sel.timelines)),
			Hint:    "Pass -t once, or list the timelines' events with 'coroner event list -t <timeline>... --json'.",
		}
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("--format %s exports one timeline, and -t selects %d", f, len(sel.timelines)),
		Hint:    "Pass -t once, or overlay the timelines with --format json, jsonl, md, mermaid or html.",
	}
}

// withoutStore returns timeline_not_found when in names a timeline in a database that does not
// exist yet, which holds none, or nil when it names none.
func (in timelineInput) withoutStore() error {
	if len(in.names) == 0 {
		return nil
	}

	return layerNotFound(fmt.Sprintf("-t %q", in.names[0]), nil)
}

// namedTimelines returns the timeline each of names selects, each once in the order named.
func namedTimelines(ctx context.Context, s *store.Store, names []string) ([]timeline.Timeline, error) {
	var timelines []timeline.Timeline
	for _, name := range names {
		t, err := findLayerTimeline(ctx, s, fmt.Sprintf("-t %q", name), name)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(timelines, func(selected timeline.Timeline) bool { return selected.ID == t.ID }) {
			timelines = append(timelines, t)
		}
	}

	return timelines, nil
}

// defaultTimeline returns the timeline a non-empty CORONER_TIMELINE or else the current timeline
// selects, and its layer, or none and "" when neither does. It reads the current timeline only
// when the variable is unset or empty.
func defaultTimeline(
	ctx context.Context, s *store.Store, lookupEnv func(string) (string, bool),
) ([]timeline.Timeline, selectedBy, error) {
	if env, _ := lookupEnv("CORONER_TIMELINE"); env != "" {
		t, err := findLayerTimeline(ctx, s, fmt.Sprintf("CORONER_TIMELINE=%q", env), env)
		return []timeline.Timeline{t}, selectedByEnv, err
	}

	t, err := s.CurrentTimeline(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, "", nil
	case err != nil:
		return nil, "", err
	}

	return []timeline.Timeline{t}, selectedByUse, nil
}

// findLayerTimeline is findTimeline for nameOrID, which layer names, reworded by layerNotFound
// when no timeline has it.
func findLayerTimeline(ctx context.Context, s *store.Store, layer, nameOrID string) (timeline.Timeline, error) {
	t, err := findTimeline(ctx, s, nameOrID)
	notFound, ok := errors.AsType[*Error](err)
	if ok && notFound.Kind == KindTimelineNotFound {
		return t, layerNotFound(layer, notFound.Err)
	}

	return t, err
}

// layerNotFound returns timeline_not_found for layer, such as `-t "x"`, naming a timeline that
// does not exist.
func layerNotFound(layer string, err error) error {
	return &Error{
		Kind: KindTimelineNotFound, Message: layer + ": no such timeline",
		Hint: "Run 'coroner timeline list' to see the timelines.", Err: err,
	}
}

// overlayTimelines returns the timelines of the overlay called name, in its order.
func overlayTimelines(ctx context.Context, s *store.Store, name string) ([]timeline.Timeline, error) {
	o, err := s.OverlayByName(ctx, name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, &Error{
			Kind: KindOverlayNotFound, Message: fmt.Sprintf("no overlay is named %q", name),
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

// lookupTimeline returns the timeline -t, CORONER_TIMELINE or the current timeline selects, and
// its layer; by is "" and err nil when no layer selects one.
func lookupTimeline(
	ctx context.Context, s *store.Store, flag *string, lookupEnv func(string) (string, bool),
) (timeline.Timeline, selectedBy, error) {
	var in timelineInput
	if flag != nil {
		in.names = []string{*flag}
	}
	sel, err := selectTimelines(ctx, s, in, lookupEnv)
	if err != nil || sel.by == "" {
		return timeline.Timeline{}, sel.by, err
	}

	return sel.timelines[0], sel.by, nil
}

// selectTimeline is lookupTimeline for commands that need a timeline: when no layer selects
// one it is timeline_required, with a hint listing the timelines.
func selectTimeline(
	ctx context.Context, s *store.Store, flag *string, lookupEnv func(string) (string, bool),
) (timeline.Timeline, error) {
	var in timelineInput
	if flag != nil {
		in.names = []string{*flag}
	}
	sel, err := selectTimelines(ctx, s, in, lookupEnv)
	if err != nil {
		return timeline.Timeline{}, err
	}

	return sel.one(ctx, s)
}

// selectNamedTimelines returns the timeline each of names, -t's values, selects, each once in
// the order named, or, when names is empty, the one timeline selectTimeline selects.
func selectNamedTimelines(
	ctx context.Context, s *store.Store, names []string, lookupEnv func(string) (string, bool),
) ([]timeline.Timeline, error) {
	sel, err := selectTimelines(ctx, s, timelineInput{names: names, rule: timelineRule{several: true}}, lookupEnv)
	if err != nil {
		return nil, err
	}

	return sel.all(ctx, s)
}

// timelineRequiredError returns timeline_required, with a hint naming the first few timelines
// there are, or 'coroner timeline create' when there are none.
func timelineRequiredError(ctx context.Context, s *store.Store) error {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return err
	}

	if len(timelines) == 0 {
		return &Error{
			Kind: KindTimelineRequired, Message: "no timeline selected",
			Hint: "Run 'coroner timeline create <name>' to create one.",
		}
	}

	const shown = 10
	names := make([]string, 0, shown)
	for _, t := range timelines[:min(len(timelines), shown)] {
		names = append(names, t.Name)
	}
	if more := len(timelines) - len(names); more > 0 {
		names = append(names, fmt.Sprintf("and %d more", more))
	}
	hint := "Timelines: " + strings.Join(names, ", ") + "\n" +
		"Pass -t <name>, set CORONER_TIMELINE, or run 'coroner timeline use <name>'."

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
