package cli

import (
	"context"
	"slices"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// mergeMode says whether merge writes its plan.
type mergeMode int

const (
	mergeWrite mergeMode = iota
	mergePlanOnly
)

// mergeInput is what merge plans from: planImport's input, whose guard merge selects from
// timelines inside its transaction.
type mergeInput struct {
	importInput
	timelines timelineInput
	lookupEnv func(string) (string, bool)
}

// mergeOutcome is what merging records did, or would do.
type mergeOutcome struct {
	plan importPlan
	// guard is the timeline every record was held to, or nil.
	guard *timeline.Timeline
	// inserted are the plan's new events as written, with their seqs, or with seq 0 unwritten.
	inserted []timeline.Event
}

// merge plans in's records against s inside one transaction and, under mergeWrite, writes the
// plan in it unless the plan refuses to write.
func merge(ctx context.Context, s *store.Store, in mergeInput, mode mergeMode) (mergeOutcome, error) {
	ids := replayIDs{draw: in.newID}
	var out mergeOutcome
	plan := func(tx *store.Tx) error {
		ids.rewind()
		out = mergeOutcome{}
		guard, err := mergeGuard(ctx, tx, in.timelines, in.lookupEnv)
		if err != nil {
			return err
		}
		input := in.importInput
		input.newID = ids.newID
		if len(guard) > 0 {
			out.guard = &guard[0]
			input.guard = out.guard
		}
		out.plan, err = planImport(ctx, input, tx)
		return err
	}

	if mode == mergePlanOnly {
		err := s.Read(ctx, plan)
		if err != nil {
			return mergeOutcome{}, err
		}
		out.inserted = slices.Clone(out.plan.write.Events)
		for i := range out.inserted {
			// An unwritten event has no seq, so a preview prints null as event add --dry-run does.
			out.inserted[i].Seq = 0
		}

		return out, nil
	}

	_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Blobs: sidecarBlobs(in.sidecars)}, func(tx *store.Tx) error {
		err := plan(tx)
		if err != nil || !out.plan.canWrite() {
			return err
		}
		out.inserted, err = tx.Import(ctx, out.plan.write)
		return err
	})
	if err != nil {
		return mergeOutcome{}, err
	}

	return out, nil
}

// mergeGuard returns the timeline in selects for every record to belong to: under the guard
// rule only the one -t names, if any, and otherwise the one selectOne picks.
func mergeGuard(ctx context.Context, s records, in timelineInput, lookupEnv func(string) (string, bool)) ([]timeline.Timeline, error) {
	if in.rule.guard {
		sel, err := selectTimelines(ctx, s, in, lookupEnv)
		return sel.timelines, err
	}
	t, err := selectOne(ctx, s, in, lookupEnv)
	if err != nil {
		return nil, err
	}

	return []timeline.Timeline{t}, nil
}
