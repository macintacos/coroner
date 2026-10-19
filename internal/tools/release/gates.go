package main

import (
	"context"
	"slices"
)

type gatesResult struct {
	OK  bool   `json:"ok"`
	SHA string `json:"sha"`
}

type secret struct {
	Name string `json:"name"`
}

type ciRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// gates checks that HEAD is ready to release, stopping at the first check that fails, and
// returns HEAD's full SHA.
func gates(ctx context.Context, sys system, args []string) (any, error) {
	if len(args) > 0 {
		return nil, usageError("gates takes no arguments; %s", usageLine)
	}

	branch, err := git(ctx, sys, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	if branch != "main" {
		return nil, &toolError{Code: "not_on_main", Message: "on " + branch + ", not main"}
	}

	status, err := git(ctx, sys, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if status != "" {
		return nil, &toolError{Code: "dirty_tree", Message: "the working tree has changes:\n" + status}
	}

	sha, err := levelWithOrigin(ctx, sys)
	if err != nil {
		return nil, err
	}

	err = githubGates(ctx, sys, sha)
	if err != nil {
		return nil, err
	}

	for _, task := range []struct{ name, code string }{
		{"preflight", "preflight_failed"},
		{"release:check", "release_check_failed"},
		{"release:snapshot", "release_snapshot_failed"},
	} {
		err = sys.run(ctx, "mise", "run", task.name)
		if err != nil {
			return nil, &toolError{Code: task.code, Message: err.Error()}
		}
	}

	return gatesResult{OK: true, SHA: sha}, nil
}

// levelWithOrigin fetches main and returns HEAD's SHA when it equals origin/main's.
func levelWithOrigin(ctx context.Context, sys system) (string, error) {
	_, err := git(ctx, sys, "fetch", "origin", "main")
	if err != nil {
		return "", err
	}

	sha, err := resolveSHA(ctx, sys, "HEAD")
	if err != nil {
		return "", err
	}
	origin, err := resolveSHA(ctx, sys, "origin/main")
	if err != nil {
		return "", err
	}
	if sha != origin {
		return "", &toolError{Code: "not_level_with_origin", Message: "HEAD is " + sha + " but origin/main is " + origin}
	}

	return sha, nil
}

// githubGates checks that gh is signed in, the tap token secret is set, and CI passed on sha.
func githubGates(ctx context.Context, sys system, sha string) error {
	_, err := sys.output(ctx, "gh", "auth", "status", "--hostname", "github.com", "--active")
	if err != nil {
		return &toolError{Code: "gh_unauthenticated", Message: err.Error()}
	}

	secrets, err := ghJSON[[]secret](ctx, sys, "secrets", "secret", "list", "-R", repo, "--json", "name")
	if err != nil {
		return err
	}
	if !slices.Contains(secrets, secret{Name: "HOMEBREW_TAP_GITHUB_TOKEN"}) {
		return &toolError{Code: "tap_secret_missing", Message: "HOMEBREW_TAP_GITHUB_TOKEN is not set on " + repo}
	}

	runs, err := ghJSON[[]ciRun](ctx, sys, "CI runs", "run", "list", "-R", repo, "--workflow", "ci.yml", "--commit", sha, "--json", "status,conclusion", "--limit", "1")
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return &toolError{Code: "ci_not_green", Message: "no ci.yml run on " + sha}
	}
	if runs[0].Status != "completed" || runs[0].Conclusion != "success" {
		return &toolError{Code: "ci_not_green", Message: "the latest ci.yml run on " + sha + " is " + runs[0].Status + "/" + runs[0].Conclusion}
	}

	return nil
}
