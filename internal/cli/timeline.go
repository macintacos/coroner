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

// The characters and length of timeline names, sources and tags; keep in sync with
// internal/timeline/grammar.go.
const (
	slugAlphanumerics = "abcdefghijklmnopqrstuvwxyz0123456789"
	namePunct         = "._-"
	sourceTagPunct    = "._:/-"
	maxSlugLength     = 64
)

func newTimelineCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "timeline",
		Short:   "Create, list and switch between timelines",
		GroupID: coreCommands,
		Long: "A timeline is the named chronology of one investigation. Create one per incident or\n" +
			"ticket, then use it so later commands know where events go.",
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --ref INC-4821\n" +
			"  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline list",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(
		newTimelineCreateCommand(ctx, app),
		newTimelineListCommand(ctx, app),
		newTimelineViewCommand(ctx, app),
		newTimelineExportCommand(ctx, app),
		newTimelineUseCommand(ctx, app),
		newTimelineEditCommand(ctx, app),
		newTimelineDeleteCommand(ctx, app),
	)

	return cmd
}

// timelineJSON is the canonical timeline object's keys. updated_at is left out: adding a key
// later is additive, removing one is not.
type timelineJSON struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Refs      []string `json:"refs"`
	CreatedAt string   `json:"created_at"`
}

func newTimelineJSON(t timeline.Timeline) timelineJSON {
	return timelineJSON{
		ID:    t.ID,
		Name:  t.Name,
		Title: t.Title,
		// Never nil, so an empty list encodes as [] rather than null.
		Refs:      append([]string{}, t.Refs...),
		CreatedAt: timeline.FormatInstant(t.CreatedAt),
	}
}

// findTimeline returns the timeline called nameOrID, or else the one with that id. A name
// that is another timeline's id is ambiguous.
func findTimeline(ctx context.Context, s *store.Store, nameOrID string) (timeline.Timeline, error) {
	t, err := s.TimelineByName(ctx, nameOrID)
	if errors.Is(err, store.ErrNotFound) {
		t, err = s.Timeline(ctx, nameOrID)
	} else if err == nil {
		err = checkUnambiguous(ctx, s, t, nameOrID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return t, &Error{
			Kind:    KindTimelineNotFound,
			Message: fmt.Sprintf("no timeline is named %q or has that id", nameOrID),
			Hint:    "Run 'coroner timeline list' to see the timelines.",
			Err:     err,
		}
	}

	return t, err
}

// checkUnambiguous returns ambiguous_id when a timeline other than named has nameOrID as its
// id.
func checkUnambiguous(ctx context.Context, s *store.Store, named timeline.Timeline, nameOrID string) error {
	byID, err := s.Timeline(ctx, nameOrID)
	switch {
	case errors.Is(err, store.ErrNotFound) || (err == nil && byID.ID == named.ID):
		return nil
	case err != nil:
		return err
	}

	return &Error{
		Kind:    KindAmbiguousID,
		Message: fmt.Sprintf("`%s` is the name of timeline %s and the id of timeline `%s`", nameOrID, named.ID, byID.Name),
		Hint:    fmt.Sprintf("Pass %s for the first, or `%s` for the second.", named.ID, byID.Name),
	}
}

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
// on every command; checkOneTimeline refuses a second timeline.
func timelineField() field {
	return field{name: "timeline", shorthand: "t", typ: stringsField, usage: timelineUsage, check: withoutEnv(checkTimelineFlag)}
}

// overlayAnnotation is the cmd.Annotations key marking a command that reads every timeline a
// repeated -t names. Any other command's -t names one timeline.
const overlayAnnotation = "overlay"

// checkOneTimeline returns usage when -t is empty (A-9), or is given more than one value for a
// command without overlayAnnotation. It runs before the store opens, so a timeline's name and
// its id are two values.
func checkOneTimeline(cmd *cobra.Command) error {
	if cmd.Flags().Lookup("timeline") == nil || cmd.Annotations[overlayAnnotation] == "true" {
		return nil
	}
	names, err := timelineFlags(cmd.Flags())
	if err != nil || len(names) <= 1 {
		return err
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("-t is given %d different values, and %s acts on one timeline", len(names), cmd.CommandPath()),
		Hint:    "Pass -t once. 'coroner event list' and 'coroner timeline export' read several timelines.",
	}
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
	names := appendUnique(nil, stringArray(flags, "timeline")...)
	for _, name := range names {
		err := checkTimelineFlag(name)
		if err != nil {
			return nil, err
		}
	}

	return names, nil
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

func timelineIDs(timelines []timeline.Timeline) []string {
	ids := make([]string, 0, len(timelines))
	for _, t := range timelines {
		ids = append(ids, t.ID)
	}

	return ids
}

// readEach returns what read returns for the id of each of timelines, in order.
func readEach[T any](timelines []timeline.Timeline, read func(timelineID string) ([]T, error)) ([]T, error) {
	var all []T
	for _, t := range timelines {
		records, err := read(t.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
	}

	return all, nil
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

// checkTimeline returns the first rule t breaks as an *Error: an invalid name as invalid_name
// with a suggested name, anything else as usage.
func checkTimeline(t timeline.Timeline) error {
	err := checkName(t.Name)
	if err != nil {
		return err
	}

	err = t.Validate()
	if err != nil {
		return &Error{Kind: KindUsage, Message: err.Error(), Hint: "A ref is a ticket key or URL.", Err: err}
	}

	return nil
}

// checkName returns invalid_name, with a suggested name, unless name is a valid timeline name.
func checkName(name string) error {
	err := timeline.ValidateTimelineName(name)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_' and '-'; put free text in --title."
	if suggestion := suggestName(name); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`? Put free text in --title.", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// suggestName returns the valid timeline name checkName suggests for name, or "" when name is valid
// or nothing valid remains of it.
func suggestName(name string) string {
	if timeline.ValidateTimelineName(name) == nil {
		return ""
	}

	return suggestSlug(name, namePunct, timeline.ValidateTimelineName)
}

// withOneArg returns run behind cobra.ExactArgs(1), failing as Run reports cobra's own rejection,
// for a command whose form opens without its argument.
func withOneArg(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := cobra.ExactArgs(1)(cmd, args)
		if err != nil {
			return oneArgError(cmd, err)
		}

		return run(cmd, args)
	}
}

// oneArgOrNone accepts no argument, which a form asks for, and rejects more than one as
// cobra.ExactArgs(1) does.
func oneArgOrNone(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return oneArgError(cmd, cobra.ExactArgs(1)(cmd, args))
	}

	return nil
}

// oneArgError is usageError with the usage line in its hint, as cobra's message never names the
// argument to pass.
func oneArgError(cmd *cobra.Command, err error) *Error {
	failure := usageError(cmd, err)
	failure.Hint = fmt.Sprintf("Usage: %s\nRun '%s --help' for more.", cmd.UseLine(), cmd.CommandPath())
	return failure
}

func timelineExistsError(name, hint string, err error) *Error {
	return &Error{Kind: KindTimelineExists, Message: fmt.Sprintf("timeline %q already exists", name), Hint: hint, Err: err}
}

// suggestSlug returns input made into a slug of letters, digits and punct (lowercased, other
// characters dashed, trimmed, cut to 64) that valid accepts, or "" when nothing valid remains.
func suggestSlug(input, punct string, valid func(string) error) string {
	var b strings.Builder
	for _, r := range strings.ToLower(input) {
		if !strings.ContainsRune(slugAlphanumerics+punct, r) {
			r = '-'
		}
		if r == '-' && strings.HasSuffix(b.String(), "-") {
			continue
		}
		b.WriteRune(r)
	}

	slug := strings.TrimLeft(b.String(), punct)
	slug = strings.TrimRight(slug[:min(len(slug), maxSlugLength)], "-")
	if valid(slug) != nil {
		return ""
	}

	return slug
}

// appendUnique appends each value in add that values does not hold yet.
func appendUnique(values []string, add ...string) []string {
	for _, value := range add {
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}

	return values
}

// stringArray returns the values of the repeatable flag called name as given: pflag's
// GetStringArray round-trips them through CSV, which drops a lone empty value.
func stringArray(flags *pflag.FlagSet, name string) []string {
	values, _ := flags.Lookup(name).Value.(pflag.SliceValue)
	return values.GetSlice()
}

// printTimeline prints t when o asks for JSON; text output leaves it to the command's message.
func printTimeline(o output, t timeline.Timeline) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newTimelineJSON(t))
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
