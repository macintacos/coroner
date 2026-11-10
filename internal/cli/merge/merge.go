// Package merge plans how exported records merge into a database and writes that plan, for
// import and event add --from-file.
package merge

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Mode says whether Merge writes its plan.
type Mode int

// Write writes the plan unless it refuses to; PlanOnly never writes.
const (
	Write Mode = iota
	PlanOnly
)

// Input is what Merge plans from: PlanImport's input, whose guard Merge selects from
// timelines inside its transaction.
type Input struct {
	ImportInput
	Timelines clikit.TimelineInput
	LookupEnv func(string) (string, bool)
	Logger    *slog.Logger
}

// Outcome is what merging records did, or would do.
type Outcome struct {
	Plan ImportPlan
	// Guard is the timeline every record was held to, or nil.
	Guard *timeline.Timeline
	// Inserted are the plan's new events: as written, with their seqs, under Write (none when
	// the plan could not write), and with seq 0 under PlanOnly.
	Inserted []timeline.Event
}

// Merge plans in's records against s inside one transaction and, under Write, writes the
// plan in it unless the plan refuses to write.
func Merge(ctx context.Context, s *store.Store, in Input, mode Mode) (Outcome, error) {
	ids := clikit.ReplayIDs{Draw: in.NewID}
	var out Outcome
	planInTx := func(tx *store.Tx) error {
		ids.Rewind()
		out = Outcome{}
		var err error
		out.Guard, err = mergeGuard(ctx, in.Logger, tx, in.Timelines, in.LookupEnv)
		if err != nil {
			return err
		}
		input := in.ImportInput
		input.NewID = ids.NewID
		input.guard = out.Guard
		out.Plan, err = PlanImport(ctx, input, tx)
		return err
	}

	if mode == PlanOnly {
		err := s.Read(ctx, planInTx)
		if err != nil {
			return Outcome{}, err
		}
		out.Inserted = slices.Clone(out.Plan.write.Events)
		for i := range out.Inserted {
			// An unwritten event has no seq, so a preview prints null as event add --dry-run does.
			out.Inserted[i].Seq = 0
		}

		return out, nil
	}

	_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Blobs: SidecarBlobs(in.Sidecars, in.LocalBlobs)}, func(tx *store.Tx) error {
		err := planInTx(tx)
		if err != nil || !out.Plan.CanWrite() {
			return err
		}
		out.Inserted, err = tx.Import(ctx, out.Plan.write)
		return err
	})
	if err != nil {
		return Outcome{}, err
	}

	return out, nil
}

// mergeGuard returns the timeline in selects for every record to belong to: under the guard
// rule only the one -t names, or nil when it names none, and otherwise the one SelectOne picks.
func mergeGuard(
	ctx context.Context, logger *slog.Logger, s clikit.Records, in clikit.TimelineInput, lookupEnv func(string) (string, bool),
) (*timeline.Timeline, error) {
	if in.Rule.Guard {
		sel, err := clikit.SelectTimelines(ctx, logger, s, in, lookupEnv)
		if err != nil || len(sel.Timelines) == 0 {
			return nil, err
		}
		return &sel.Timelines[0], nil
	}
	t, err := clikit.SelectOne(ctx, logger, s, in, lookupEnv)
	if err != nil {
		return nil, err
	}

	return &t, nil
}

// SidecarBlobs returns the contents of the sidecars whose blob local does not hold.
func SidecarBlobs(sidecars map[string][]byte, local map[string]bool) []io.Reader {
	var blobs []io.Reader
	for sha, content := range sidecars {
		if !local[sha] {
			blobs = append(blobs, bytes.NewReader(content))
		}
	}

	return blobs
}
