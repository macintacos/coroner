package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/timeline"
)

func newEventCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "event",
		Short:   "Add events to a timeline",
		GroupID: coreCommands,
		Long: "An event is something that happened at a moment or over an interval, as a source reported\n" +
			"it. Events go to the timeline -t names, else CORONER_TIMELINE, else the one\n" +
			"'coroner timeline use' picked.",
		Example: "  $ coroner event add \"Primary stepped down\" --at 2026-09-11T03:14:07Z --source mongod",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	addTimelineFlag(cmd.PersistentFlags())
	cmd.AddCommand(newEventAddCommand(ctx, app), newEventListCommand(ctx, app), newEventViewCommand(ctx, app))

	return cmd
}

// eventJSON is the canonical event object's keys, every one always present.
type eventJSON struct {
	ID             string   `json:"id"`
	TimelineID     string   `json:"timeline_id"`
	Seq            *int64   `json:"seq"`
	OccurredAt     string   `json:"occurred_at"`
	OccurredOffset *int     `json:"occurred_offset"`
	Precision      string   `json:"precision"`
	Approximate    bool     `json:"approximate"`
	OccurredRaw    *string  `json:"occurred_raw"`
	EndedAt        *string  `json:"ended_at"`
	Source         *string  `json:"source"`
	Tags           []string `json:"tags"`
	Text           string   `json:"text"`
	URL            *string  `json:"url"`
	Author         *string  `json:"author"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// newEventJSON returns e's JSON object; seq is null until the store allocates one.
func newEventJSON(e timeline.Event) eventJSON {
	j := eventJSON{
		ID:             e.ID,
		TimelineID:     e.TimelineID,
		OccurredAt:     timeline.FormatInstant(e.OccurredAt),
		OccurredOffset: e.OccurredOffset,
		Precision:      string(e.Precision),
		Approximate:    e.Approximate,
		OccurredRaw:    e.OccurredRaw,
		Source:         e.Source,
		// Never nil, so no tags encodes as [] rather than null.
		Tags:      append([]string{}, e.Tags...),
		Text:      e.Text,
		URL:       e.URL,
		Author:    e.Author,
		CreatedAt: timeline.FormatInstant(e.CreatedAt),
		UpdatedAt: timeline.FormatInstant(e.UpdatedAt),
	}
	if e.Seq > 0 {
		j.Seq = &e.Seq
	}
	if e.EndedAt != nil {
		j.EndedAt = new(timeline.FormatInstant(*e.EndedAt))
	}

	return j
}
