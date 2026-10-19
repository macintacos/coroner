package main

import (
	"context"
	"encoding/json/v2"
	"errors"
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
	err := flags.Parse(args)
	if err != nil || flags.NArg() > 0 || *name == "" || *rev == "" {
		return nil, usageError("%s", usageLine)
	}

	sha, err := resolveSHA(ctx, sys, *rev)
	if err != nil {
		return nil, err
	}
	id, err := findReleaseRun(ctx, sys, *name, sha)
	if err != nil {
		return nil, err
	}

	watchCtx, cancel := context.WithTimeout(ctx, watchDeadline)
	defer cancel()
	err = sys.run(watchCtx, "gh", "run", "watch", id, "-R", repo, "--exit-status", "--interval", "15")
	if errors.Is(err, context.DeadlineExceeded) || watchCtx.Err() != nil {
		return nil, &toolError{Code: "workflow_timeout", Message: "release.yml run " + id + " did not finish within " + watchDeadline.String()}
	}

	// A failed watch is judged by the run's conclusion below.
	out, err := gh(ctx, sys, "run", "view", id, "-R", repo, "--json", "conclusion,url")
	if err != nil {
		return nil, err
	}
	var outcome runOutcome
	err = json.Unmarshal([]byte(out), &outcome)
	if err != nil {
		return nil, &toolError{Code: "github_error", Message: "reading the run: " + err.Error()}
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
		out, err := gh(ctx, sys, "run", "list", "-R", repo, "--workflow", "release.yml", "--commit", sha, "--event", "push", "--json", "databaseId,headBranch,url")
		if err != nil {
			return "", err
		}
		var runs []releaseRun
		err = json.Unmarshal([]byte(out), &runs)
		if err != nil {
			return "", &toolError{Code: "github_error", Message: "reading release runs: " + err.Error()}
		}
		for _, run := range runs {
			if run.HeadBranch == name {
				return strconv.Itoa(run.DatabaseID), nil
			}
		}
	}

	return "", &toolError{Code: "workflow_not_found", Message: "no release.yml run for " + name + " appeared"}
}
