package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// nameCharacters are the characters a timeline name may hold.
const nameCharacters = "abcdefghijklmnopqrstuvwxyz0123456789._-"

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
		RunE: runNoun,
	}

	cmd.AddCommand(
		newTimelineCreateCommand(ctx, app),
		newTimelineListCommand(ctx, app),
		newTimelineViewCommand(ctx, app),
		newTimelineUseCommand(ctx, app),
		newTimelineEditCommand(ctx, app),
		newTimelineDeleteCommand(ctx, app),
	)

	return cmd
}

// timelineJSON is a timeline as commands print it under --json.
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

// findTimeline returns the timeline called nameOrID, or else the one with that id.
func findTimeline(ctx context.Context, s *store.Store, nameOrID string) (timeline.Timeline, error) {
	t, err := s.TimelineByName(ctx, nameOrID)
	if errors.Is(err, store.ErrNotFound) {
		t, err = s.Timeline(ctx, nameOrID)
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

// checkTimeline returns an *Error for every way t breaks the domain rules, suggesting a
// valid name for an invalid one.
func checkTimeline(t timeline.Timeline) error {
	err := timeline.ValidateTimelineName(t.Name)
	if err != nil {
		hint := "Use lowercase letters, digits, '.', '_' and '-'; put free text in --title."
		if suggestion := suggestName(t.Name); suggestion != "" {
			hint = fmt.Sprintf("Did you mean `%s`? Put free text in --title.", suggestion)
		}

		return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
	}

	err = t.Validate()
	if err != nil {
		return &Error{Kind: KindUsage, Message: err.Error(), Hint: "A ref is a ticket key or URL.", Err: err}
	}

	return nil
}

// suggestName returns the valid timeline name closest to input, or "" when none is close.
// It never replaces the input: the user decides.
func suggestName(input string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(input) {
		if !strings.ContainsRune(nameCharacters, r) {
			r = '-'
		}
		if r == '-' && strings.HasSuffix(b.String(), "-") {
			continue
		}
		b.WriteRune(r)
	}

	name := strings.TrimLeft(b.String(), "._-")
	name = strings.TrimRight(name[:min(len(name), 64)], "-")
	if timeline.ValidateTimelineName(name) != nil {
		return ""
	}

	return name
}

// appendRefs appends each ref in add that refs does not hold yet.
func appendRefs(refs []string, add ...string) []string {
	for _, ref := range add {
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}

	return refs
}

// printsJSON reports whether cmd was asked for JSON. The format is already validated by the
// time a command body runs.
func printsJSON(cmd *cobra.Command) bool {
	chosen, _ := outputFormat(cmd)
	return chosen == formatJSON
}

// printChanged prints t as JSON when cmd was asked for JSON, then message.
func printChanged(cmd *cobra.Command, app App, t timeline.Timeline, message string) error {
	if printsJSON(cmd) {
		err := writeJSON(app.Out, app.OutIsTTY, newTimelineJSON(t))
		if err != nil {
			return err
		}
	}

	return notify(cmd, app, message)
}

// notify writes message, meant for people, to app.Err unless --quiet was given.
func notify(cmd *cobra.Command, app App, message string) error {
	if quiet, _ := cmd.Flags().GetBool("quiet"); quiet {
		return nil
	}

	_, err := io.WriteString(app.Err, message)
	return err
}
