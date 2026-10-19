package main

import (
	"cmp"
	"context"
	"flag"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// prNumber matches the (#N) a squash merge appends to its subject.
var prNumber = regexp.MustCompile(`\(#(\d+)\)$`)

// deprecationCall matches a line that calls deprecateCommand or deprecateFlag, and their
// declarations, which parseDeprecations filters out.
var deprecationCall = regexp.MustCompile(`deprecate(Command|Flag)\(`)

type commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

type pullRequest struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Body      string   `json:"body"`
	Stability string   `json:"stability"`
	Commits   []commit `json:"commits"`
}

type contractFile struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type deprecation struct {
	Change string `json:"change"`
	Path   string `json:"path"`
	Line   string `json:"line"`
}

type contractHints struct {
	Files        []contractFile `json:"files"`
	Deprecations []deprecation  `json:"deprecations"`
}

type computeResult struct {
	OK               bool          `json:"ok"`
	Bump             string        `json:"bump"`
	RC               bool          `json:"rc"`
	SHA              string        `json:"sha"`
	PreviousTag      *string       `json:"previous_tag"`
	Tag              string        `json:"tag"`
	CompareURL       *string       `json:"compare_url"`
	PullRequests     []pullRequest `json:"pull_requests"`
	CommitsWithoutPR []commit      `json:"commits_without_pr"`
	ContractHints    contractHints `json:"contract_hints"`
}

// compute works out the next tag and gathers what the agent needs to write its notes: the
// pull requests and commits since the latest final tag, and the contract changes among them.
func compute(ctx context.Context, sys system, args []string) (any, error) {
	if len(args) == 0 || !slices.Contains([]string{"patch", "minor", "major"}, args[0]) {
		return nil, usageError("compute needs a bump of patch, minor or major; %s", usageLine)
	}
	result := computeResult{OK: true, Bump: args[0]}

	flags := flag.NewFlagSet("compute", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&result.RC, "rc", false, "tag a release candidate")
	rev := flags.String("sha", "", "the commit to release")
	err := parseFlags(flags, args[1:], "sha")
	if err != nil {
		return nil, err
	}

	sha, err := resolveSHA(ctx, sys, *rev)
	if err != nil {
		return nil, err
	}
	result.SHA = sha

	lsRemote, err := git(ctx, sys, "ls-remote", "--tags", "origin")
	if err != nil {
		return nil, err
	}
	tags := parseLsRemote(lsRemote)
	tagNames := slices.Collect(maps.Keys(tags))

	result.Tag, err = nextTag(tagNames, result.Bump, result.RC)
	if err != nil {
		return nil, usageError("%s", err)
	}

	var base string
	logRange := sha
	if latest, ok := latestFinal(tagNames); ok {
		previous := latest.String()
		compareURL := "https://github.com/" + repo + "/compare/" + previous + "..." + result.Tag
		result.PreviousTag, result.CompareURL = &previous, &compareURL
		base = tags[previous]
		logRange = base + ".." + sha
	} else {
		base, err = git(ctx, sys, "hash-object", "-t", "tree", "/dev/null")
		if err != nil {
			return nil, err
		}
	}

	log, err := git(ctx, sys, "log", "--format=%H%x1f%s", logRange)
	if err != nil {
		return nil, err
	}
	result.PullRequests, result.CommitsWithoutPR, err = pullRequests(ctx, sys, parseLog(log))
	if err != nil {
		return nil, err
	}

	result.ContractHints, err = gatherContractHints(ctx, sys, base, sha)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func resolveSHA(ctx context.Context, sys system, rev string) (string, error) {
	return git(ctx, sys, "rev-parse", "--verify", rev+"^{commit}")
}

// pullRequests groups commits under the pull request their subject names, fetching each one,
// and returns the commits that name none.
func pullRequests(ctx context.Context, sys system, commits []commit) ([]pullRequest, []commit, error) {
	var prs []pullRequest
	var orphans []commit
	positionByNumber := map[int]int{}
	for _, c := range commits {
		match := prNumber.FindStringSubmatch(c.Subject)
		if match == nil {
			orphans = append(orphans, c)
			continue
		}

		number, _ := strconv.Atoi(match[1])
		i, seen := positionByNumber[number]
		if !seen {
			pr, err := fetchPullRequest(ctx, sys, match[1])
			if err != nil {
				return nil, nil, err
			}
			i = len(prs)
			positionByNumber[number] = i
			prs = append(prs, pr)
		}
		prs[i].Commits = append(prs[i].Commits, c)
	}

	return prs, orphans, nil
}

func fetchPullRequest(ctx context.Context, sys system, number string) (pullRequest, error) {
	pr, err := ghJSON[pullRequest](ctx, sys, "pull request "+number, "pr", "view", number, "-R", repo, "--json", "number,title,body,url")
	if err != nil {
		return pullRequest{}, err
	}
	pr.Stability = stability(pr.Body)

	return pr, nil
}

func gatherContractHints(ctx context.Context, sys system, base, sha string) (contractHints, error) {
	// Pin what a user's diff.external, diff.noprefix or color.diff would change.
	plain := []string{"diff", "--no-ext-diff", "--no-color", "--src-prefix=a/", "--dst-prefix=b/"}
	nameStatus, err := git(ctx, sys, append(plain, "--name-status", "--no-renames", base, sha, "--", "internal/cli/testdata/script/contract", "schema")...)
	if err != nil {
		return contractHints{}, err
	}

	diff, err := git(ctx, sys, append(plain, "-U0", "--no-renames", base, sha, "--", "internal/cli", ":(exclude)internal/cli/*_test.go")...)
	if err != nil {
		return contractHints{}, err
	}

	return contractHints{Files: parseNameStatus(nameStatus), Deprecations: parseDeprecations(diff)}, nil
}

// parseLsRemote maps each tag in git ls-remote output to its commit, preferring the peeled
// ^{} line an annotated tag adds over the tag object's own.
func parseLsRemote(out string) map[string]string {
	tags := map[string]string{}
	for line := range strings.Lines(out) {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		name, isTag := strings.CutPrefix(ref, "refs/tags/")
		if !ok || !isTag {
			continue
		}
		if peeled, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
			tags[peeled] = sha
		} else if _, seen := tags[name]; !seen {
			tags[name] = sha
		}
	}

	return tags
}

// parseLog reads git log --format=%H%x1f%s output.
func parseLog(out string) []commit {
	var commits []commit
	for line := range strings.Lines(out) {
		sha, subject, ok := strings.Cut(strings.TrimRight(line, "\n"), "\x1f")
		if ok {
			commits = append(commits, commit{SHA: sha, Subject: subject})
		}
	}

	return commits
}

// stability reads the pull request template's "`stability:` ___" line: the word filled in, or
// none when its box is checked, unfilled when neither, and missing when the line is absent.
func stability(body string) string {
	for line := range strings.Lines(body) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [") && !strings.HasPrefix(trimmed, "* [") {
			continue
		}
		lower := strings.ToLower(strings.ReplaceAll(line, "`", ""))
		_, after, found := strings.Cut(lower, "stability:")
		if !found {
			continue
		}

		if words := strings.Fields(after); len(words) > 0 && slices.Contains([]string{"breaking", "additive", "experimental"}, words[0]) {
			return words[0]
		}
		if strings.Contains(lower, "[x]") {
			return "none"
		}

		return "unfilled"
	}

	return "missing"
}

// parseNameStatus reads git diff --name-status output.
func parseNameStatus(out string) []contractFile {
	statuses := map[string]string{"A": "added", "M": "modified", "D": "deleted"}
	var files []contractFile
	for line := range strings.Lines(out) {
		letter, path, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		files = append(files, contractFile{Status: cmp.Or(statuses[letter], letter), Path: path})
	}

	return files
}

// parseDeprecations finds the deprecateCommand and deprecateFlag calls a git diff -U0 adds or
// removes. A call spans lines, so only its opening line is kept.
func parseDeprecations(diff string) []deprecation {
	var deprecations []deprecation
	path := ""
	for line := range strings.Lines(diff) {
		line = strings.TrimRight(line, "\n")
		// A deleted file's new side is +++ /dev/null, so its path comes from the old side.
		if p, ok := strings.CutPrefix(line, "--- a/"); ok {
			path = p
			continue
		}
		if p, ok := strings.CutPrefix(line, "+++ b/"); ok {
			path = p
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}

		change := "added"
		text, ok := strings.CutPrefix(line, "+")
		if !ok {
			change = "removed"
			text, ok = strings.CutPrefix(line, "-")
		}
		text = strings.TrimSpace(text)
		if ok && deprecationCall.MatchString(text) && !strings.HasPrefix(text, "func deprecate") {
			deprecations = append(deprecations, deprecation{Change: change, Path: path, Line: text})
		}
	}

	return deprecations
}
