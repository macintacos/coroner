package main

import (
	"context"
	"flag"
	"io"
	"strconv"
	"time"
)

const (
	findTries     = 40
	findInterval  = 15 * time.Second
	watchDeadline = 40 * time.Minute // the queue plus release.yml's own 30-minute job limit
)

type watchResult struct {
	OK     bool   `json:"ok"`
	Tag    string `json:"tag"`
	RunURL string `json:"run_url"`
}

type releaseRun struct {
	DatabaseID int    `json:"databaseId"`
	HeadBranch string `json:"headBranch"`
	URL        string `json:"url"`
}

type runOutcome struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// watch waits for the release workflow run the tag's push started, and fails unless it
// succeeds.
func watch(ctx context.Context, sys system, args []string) (any, error) {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("tag", "", "the pushed tag")
	rev := flags.String("sha", "", "the tagged commit")
	err := parseFlags(flags, args, "tag", "sha")
	if err != nil {
		return nil, err
	}

	sha, err := resolveSHA(ctx, sys, *rev)
	if err != nil {
		return nil, err
	}
	runID, err := findReleaseRun(ctx, sys, *name, sha)
	if err != nil {
		return nil, err
	}

	watchCtx, cancel := context.WithTimeout(ctx, watchDeadline)
	defer cancel()
	watchErr := sys.run(watchCtx, "gh", "run", "watch", runID, "-R", repo, "--exit-status", "--interval", strconv.Itoa(int(findInterval.Seconds())))
	if watchCtx.Err() != nil {
		return nil, &toolError{Code: "workflow_timeout", Message: "release.yml run " + runID + " did not finish within " + watchDeadline.String()}
	}

	// A failed watch is judged by the run's status and conclusion below: gh run watch also
	// exits 1 on a network error while the run goes on.
	outcome, err := ghJSON[runOutcome](ctx, sys, "the run", "run", "view", runID, "-R", repo, "--json", "status,conclusion,url")
	if err != nil {
		return nil, err
	}
	if watchErr != nil && outcome.Status != "completed" {
		return nil, &toolError{Code: "watch_interrupted", Message: "the watch stopped before release.yml run " + runID + " finished: " + watchErr.Error()}
	}
	if outcome.Conclusion != "success" {
		return nil, &toolError{Code: "workflow_failed", Message: "release.yml concluded " + outcome.Conclusion + ": " + outcome.URL}
	}

	return watchResult{OK: true, Tag: *name, RunURL: outcome.URL}, nil
}

// findReleaseRun polls for the release.yml run on sha whose branch is the tag, and returns
// its id.
func findReleaseRun(ctx context.Context, sys system, name, sha string) (string, error) {
	for try := range findTries {
		if try > 0 {
			sys.sleep(findInterval)
		}
		runs, err := ghJSON[[]releaseRun](ctx, sys, "release runs", "run", "list", "-R", repo, "--workflow", "release.yml", "--commit", sha, "--event", "push", "--json", "databaseId,headBranch,url")
		if err != nil {
			return "", err
		}
		for _, run := range runs {
			if run.HeadBranch == name {
				return strconv.Itoa(run.DatabaseID), nil
			}
		}
	}

	return "", &toolError{Code: "workflow_not_found", Message: "no release.yml run for " + name + " appeared"}
}
