package clikit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// SelectedBy names the layer that selected the timeline a command acts on.
type SelectedBy string

// The layers that select a timeline, in precedence order.
const (
	SelectedByFlag    SelectedBy = "flag"
	SelectedByEnv     SelectedBy = "env"
	SelectedByUse     SelectedBy = "use"
	selectedByName    SelectedBy = "name"
	selectedByOverlay SelectedBy = "overlay"
)

// Setting returns how a user sets this layer: -t, CORONER_TIMELINE or 'coroner timeline use'.
func (by SelectedBy) Setting() string {
	switch by {
	case SelectedByFlag:
		return "-t"
	case SelectedByEnv:
		return "CORONER_TIMELINE"
	case SelectedByUse:
		return "'coroner timeline use'"
	case selectedByName:
		return "the timeline argument"
	case selectedByOverlay:
		return "--overlay"
	}

	return string(by)
}

// AddTimelineFlag declares -t/--timeline, so every command registers the same name,
// shorthand and usage.
// It is a StringArray, not a StringSlice, so a value is never split on commas.
func AddTimelineFlag(flags *pflag.FlagSet) {
	flags.StringArrayP("timeline", "t", nil, timelineUsage)
}

const timelineUsage = "Timeline `name|id` to act on"

// TimelineField is -t as a write command's field: a stringsField because -t is a StringArray
// on every command; wrapRunErrors refuses a second timeline on a command that reads one.
func TimelineField() Field {
	return Field{Name: "timeline", Shorthand: "t", Typ: StringsField, Title: "Timeline", Usage: timelineUsage, Check: WithoutEnv(checkTimelineFlag)}
}

// TimelinesAnnotation is the cmd.Annotations key listing, comma-separated, how a command names
// its timelines: several (a repeated -t, and --overlay where registered, overlay timelines),
// positional (a positional name selects the timeline) and guard (-t only, no environment or
// current timeline). A command without it acts on one timeline.
const TimelinesAnnotation = "timelines"

// OneTimelineFormatsAnnotation is the cmd.Annotations key listing, comma-separated, the formats
// of a several-timeline command that print one timeline.
const OneTimelineFormatsAnnotation = "oneTimelineFormats"

// TimelineRule is how many timelines a command reads, and how they may be named.
type TimelineRule struct {
	Command    string // the command path the count error names
	Several    bool
	Positional bool
	Guard      bool // -t only: no CORONER_TIMELINE or current-timeline layer
	OneFormats []Format
}

// TimelineInput is what the command line or a write command's fields name.
type TimelineInput struct {
	Names      []string
	Overlay    *string
	Positional *string
	Rule       TimelineRule
}

// RuleOf returns cmd's TimelineRule, read from its TimelinesAnnotation and
// OneTimelineFormatsAnnotation.
func RuleOf(cmd *cobra.Command) TimelineRule {
	tokens := strings.Split(cmd.Annotations[TimelinesAnnotation], ",")
	rule := TimelineRule{
		Command:    cmd.CommandPath(),
		Several:    slices.Contains(tokens, "several"),
		Positional: slices.Contains(tokens, "positional"),
		Guard:      slices.Contains(tokens, "guard"),
	}
	if formats := cmd.Annotations[OneTimelineFormatsAnnotation]; formats != "" {
		for f := range strings.SplitSeq(formats, ",") {
			rule.OneFormats = append(rule.OneFormats, Format(f))
		}
	}

	return rule
}

// ReadTimelineInput returns the timelines flags and args name under rule, or usage when -t or
// --overlay is empty (A-9), --overlay is given with -t, a positional name is given with -t, or
// a one-timeline command is given several -t values. It does no I/O, so a timeline's name and
// its id are two values.
func ReadTimelineInput(flags *pflag.FlagSet, args []string, rule TimelineRule) (TimelineInput, error) {
	in := TimelineInput{Rule: rule}
	if flags.Lookup("timeline") == nil {
		return in, nil
	}

	var err error
	in.Names, err = readTimelineNames(flags)
	if err != nil {
		return TimelineInput{}, err
	}
	in.Overlay, err = readOverlay(flags, in.Names)
	if err != nil {
		return TimelineInput{}, err
	}
	if rule.Positional && len(args) == 1 {
		if len(in.Names) > 0 {
			return TimelineInput{}, &Error{
				Kind:    KindUsage,
				Message: "pass either a timeline name or id, or -t",
				Hint:    "Run '" + rule.Command + " --help' for usage.",
			}
		}
		in.Positional = &args[0]
	}
	if !rule.Several && len(in.Names) > 1 {
		return TimelineInput{}, &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("-t is given %d different values, and %s acts on one timeline", len(in.Names), rule.Command),
			Hint: "Pass -t once. 'coroner event list', 'coroner timeline export', " +
				"'coroner timeline view' and 'coroner overlay create' read several timelines.",
		}
	}

	return in, nil
}

// readTimelineNames returns -t's values in the order given, each once, or usage when one is
// empty (A-9).
func readTimelineNames(flags *pflag.FlagSet) ([]string, error) {
	names := AppendUnique(nil, stringArray(flags, "timeline")...)
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
	overlay := ChangedString(flags, "overlay")
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

// TimelineInputOf returns the timelines -t names in a write command's or form's fields, which
// have already checked them.
func TimelineInputOf(in FieldValues) TimelineInput {
	return TimelineInput{Names: AppendUnique(nil, in.Values("timeline")...)}
}

func checkTimelineFlag(value string) error {
	if value != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: "-t must not be empty", Hint: "Pass -t with a timeline name or id."}
}

// envTimeline returns CORONER_TIMELINE, or "" when it is unset or empty.
func envTimeline(lookupEnv func(string) (string, bool)) string {
	env, _ := lookupEnv("CORONER_TIMELINE")
	return env
}

// Selection is the timelines a command's input selects and the layer that selected them: none,
// and by "", when no layer names one.
type Selection struct {
	Timelines  []timeline.Timeline
	By         SelectedBy
	oneFormats []Format
}

// SelectTimelines returns the timelines the first layer that names any selects: --overlay's, in
// the overlay's order; the positional name; -t's names, each once in the order named; then,
// unless in's rule is guard, a non-empty CORONER_TIMELINE, then the current timeline.
// ReadTimelineInput refuses -t beside --overlay or the positional name. A layer
// naming a missing timeline is timeline_not_found rather than falling through to the next, so a
// typo never acts on another timeline.
func SelectTimelines(
	ctx context.Context, logger *slog.Logger, s Records, in TimelineInput, lookupEnv func(string) (string, bool),
) (Selection, error) {
	sel := Selection{oneFormats: in.Rule.OneFormats}
	var err error
	switch {
	case in.Overlay != nil:
		sel.Timelines, err = overlayTimelines(ctx, s, *in.Overlay)
		sel.By = selectedByOverlay
	case in.Positional != nil:
		var t timeline.Timeline
		t, err = FindTimeline(ctx, s, *in.Positional)
		sel.Timelines, sel.By = []timeline.Timeline{t}, selectedByName
	case len(in.Names) > 0:
		sel.Timelines, err = namedTimelines(ctx, s, in.Names)
		sel.By = SelectedByFlag
	case !in.Rule.Guard:
		sel.Timelines, sel.By, err = defaultTimeline(ctx, s, lookupEnv)
	}
	if err != nil {
		return Selection{}, err
	}
	if sel.By != "" {
		logger.DebugContext(ctx, "timelines selected", "by", sel.By, "timeline_ids", TimelineIDs(sel.Timelines))
	}

	return sel, nil
}

// SelectOne returns the one timeline in selects, or timeline_required when none does.
func SelectOne(
	ctx context.Context, logger *slog.Logger, s Records, in TimelineInput, lookupEnv func(string) (string, bool),
) (timeline.Timeline, error) {
	sel, err := SelectTimelines(ctx, logger, s, in, lookupEnv)
	if err != nil {
		return timeline.Timeline{}, err
	}

	return sel.One(ctx, s)
}

// SelectAll returns the timelines in selects, usage when they are several and f prints one, or
// timeline_required when it selects none.
func SelectAll(
	ctx context.Context, logger *slog.Logger, s Records, in TimelineInput, lookupEnv func(string) (string, bool), f Format,
) ([]timeline.Timeline, error) {
	sel, err := SelectTimelines(ctx, logger, s, in, lookupEnv)
	if err != nil {
		return nil, err
	}
	err = sel.checkFormat(f)
	if err != nil {
		return nil, err
	}

	return sel.All(ctx, s)
}

// One returns the first timeline sel selected, the only one a one-timeline command can, or
// timeline_required when it selected none.
func (sel Selection) One(ctx context.Context, s Records) (timeline.Timeline, error) {
	if len(sel.Timelines) == 0 {
		return timeline.Timeline{}, timelineRequiredError(ctx, s)
	}

	return sel.Timelines[0], nil
}

// All returns the timelines sel selected, or timeline_required when it selected none.
func (sel Selection) All(ctx context.Context, s Records) ([]timeline.Timeline, error) {
	if len(sel.Timelines) == 0 {
		return nil, timelineRequiredError(ctx, s)
	}

	return sel.Timelines, nil
}

// checkFormat returns usage when sel holds several timelines and f is one of the formats that
// print one. Call it before reading events; it counts the deduplicated selection, so a name and
// its id count once.
// ponytail: worded by format, since view's one-timeline format (json) and export's (svg) are
// disjoint; key by command if they ever overlap.
func (sel Selection) checkFormat(f Format) error {
	if len(sel.Timelines) <= 1 || !slices.Contains(sel.oneFormats, f) {
		return nil
	}
	if f == FormatJSON {
		return &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("--format json prints one timeline, and %s selects %d", sel.By.Setting(), len(sel.Timelines)),
			Hint:    "Pass -t once, or list the timelines' events with 'coroner event list -t <timeline>... --json'.",
		}
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("--format %s exports one timeline, and %s selects %d", f, sel.By.Setting(), len(sel.Timelines)),
		Hint:    "Pass -t for one timeline, or overlay the timelines with --format json, jsonl, md, mermaid or html.",
	}
}

// WithoutStore returns timeline_not_found when in names a timeline in a database that does not
// exist yet, which holds none, or nil when it names none. It applies only under the guard rule,
// since it never consults CORONER_TIMELINE.
func (in TimelineInput) WithoutStore() error {
	if len(in.Names) == 0 {
		return nil
	}

	return layerNotFound(fmt.Sprintf("-t %q", in.Names[0]), nil)
}

// namedTimelines returns the timeline each of names selects, each once in the order named.
func namedTimelines(ctx context.Context, s Records, names []string) ([]timeline.Timeline, error) {
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
	ctx context.Context, s Records, lookupEnv func(string) (string, bool),
) ([]timeline.Timeline, SelectedBy, error) {
	if env := envTimeline(lookupEnv); env != "" {
		t, err := findLayerTimeline(ctx, s, fmt.Sprintf("CORONER_TIMELINE=%q", env), env)
		return []timeline.Timeline{t}, SelectedByEnv, err
	}

	t, err := s.CurrentTimeline(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, "", nil
	case err != nil:
		return nil, "", err
	}

	return []timeline.Timeline{t}, SelectedByUse, nil
}

// findLayerTimeline is FindTimeline for nameOrID, which layer names, reworded by layerNotFound
// when no timeline has it.
func findLayerTimeline(ctx context.Context, s Records, layer, nameOrID string) (timeline.Timeline, error) {
	t, err := FindTimeline(ctx, s, nameOrID)
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
func overlayTimelines(ctx context.Context, s Records, name string) ([]timeline.Timeline, error) {
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

	return TimelinesByID(ctx, s, o.TimelineIDs)
}

// timelineRequiredError returns timeline_required, with a hint naming the first few timelines
// there are, or 'coroner timeline create' when there are none.
func timelineRequiredError(ctx context.Context, s Records) error {
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

	names := make([]string, 0, len(timelines))
	for _, t := range timelines {
		names = append(names, t.Name)
	}
	hint := "Timelines: " + strings.Join(FirstFew(names, 10), ", ") + "\n" +
		"Pass -t <name>, set CORONER_TIMELINE, or run 'coroner timeline use <name>'."

	return &Error{Kind: KindTimelineRequired, Message: "no timeline selected", Hint: hint}
}

// ShadowNotice returns what the timeline picker says while CORONER_TIMELINE selects the
// timeline commands act on over the current timeline, or "" when the variable is unset or
// empty.
func ShadowNotice(lookupEnv func(string) (string, bool)) string {
	env := envTimeline(lookupEnv)
	if env == "" {
		return ""
	}

	return fmt.Sprintf("CORONER_TIMELINE=%q selects the timeline commands act on", env)
}

// ShadowWarning returns the warning printed once picked is stored while CORONER_TIMELINE
// selects another timeline, or "" when it is unset, empty or names picked.
func ShadowWarning(lookupEnv func(string) (string, bool), picked timeline.Timeline) string {
	env := envTimeline(lookupEnv)
	if env == "" || env == picked.Name || env == picked.ID {
		return ""
	}

	return fmt.Sprintf("CORONER_TIMELINE=%q still selects the timeline commands act on. Unset it to act on `%s`.",
		env, picked.Name)
}

// AddOverlayFlag declares --overlay, on a command whose -t reads several timelines.
func AddOverlayFlag(flags *pflag.FlagSet) {
	flags.String("overlay", "", "Read the timelines of the overlay called `name`, instead of -t")
}
