package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
)

type tagResult struct {
	OK     bool   `json:"ok"`
	Tag    string `json:"tag"`
	SHA    string `json:"sha"`
	DryRun bool   `json:"dry_run"`
	Pushed bool   `json:"pushed"`
}

type tagNotesResult struct {
	OK        bool   `json:"ok"`
	Tag       string `json:"tag"`
	NotesFile string `json:"notes_file"`
}

// tag creates an annotated tag on sha whose message is the notes file, and pushes it. When
// the push fails it deletes the local tag; it never deletes a pushed one.
func tag(ctx context.Context, sys system, args []string) (any, error) {
	flags := flag.NewFlagSet("tag", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("tag", "", "the tag to create")
	rev := flags.String("sha", "", "the commit to tag")
	notesFile := flags.String("notes-file", "", "the release notes")
	dryRun := flags.Bool("dry-run", false, "check, but create nothing")
	err := parseFlags(flags, args, "tag", "sha", "notes-file")
	if err != nil {
		return nil, err
	}

	if _, ok := parseTag(*name); !ok {
		return nil, &toolError{Code: "bad_tag", Message: *name + " is not vX.Y.Z or vX.Y.Z-rc.N"}
	}
	notes, err := os.ReadFile(*notesFile)
	if err != nil {
		return nil, usageError("reading the notes: %s", err)
	}
	if strings.TrimSpace(string(notes)) == "" {
		return nil, &toolError{Code: "empty_notes", Message: *notesFile + " is blank"}
	}

	sha, err := resolveSHA(ctx, sys, *rev)
	if err != nil {
		return nil, err
	}
	err = tagIsFree(ctx, sys, *name)
	if err != nil {
		return nil, err
	}

	result := tagResult{OK: true, Tag: *name, SHA: sha, DryRun: *dryRun}
	if *dryRun {
		return result, nil
	}

	// verbatim keeps the notes' ### headings, which git's default cleanup strips as comments;
	// --no-sign overrides a tag.gpgSign setting.
	_, err = git(ctx, sys, "tag", "-a", "--no-sign", "--cleanup=verbatim", "-F", *notesFile, *name, sha)
	if err != nil {
		return nil, err
	}
	_, err = git(ctx, sys, "push", "origin", "refs/tags/"+*name)
	if err != nil {
		_, deleteErr := git(ctx, sys, "tag", "-d", *name)
		return nil, &toolError{Code: "push_failed", Message: errors.Join(err, deleteErr).Error()}
	}
	result.Pushed = true

	return result, nil
}

func tagIsFree(ctx context.Context, sys system, name string) error {
	for _, args := range [][]string{
		{"tag", "-l", name},
		{"ls-remote", "--tags", "origin", "refs/tags/" + name},
	} {
		out, err := git(ctx, sys, args...)
		if err != nil {
			return err
		}
		if out != "" {
			return &toolError{Code: "tag_exists", Message: name + " already exists: " + out}
		}
	}

	return nil
}

// tagNotes writes the message of the annotated tag on origin to the out file.
func tagNotes(ctx context.Context, sys system, args []string) (any, error) {
	flags := flag.NewFlagSet("tag-notes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("tag", "", "the tag to read")
	out := flags.String("out", "", "the file to write the notes to")
	err := parseFlags(flags, args, "tag", "out")
	if err != nil {
		return nil, err
	}

	message, err := tagMessage(ctx, sys, *name)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(*out, []byte(message), 0o600)
	if err != nil {
		return nil, &toolError{Code: "internal", Message: "writing the notes: " + err.Error()}
	}

	return tagNotesResult{OK: true, Tag: *name, NotesFile: *out}, nil
}

// tagMessage fetches the tag from origin and returns its message, failing on a lightweight
// tag or a blank message.
func tagMessage(ctx context.Context, sys system, name string) (string, error) {
	ref := "refs/tags/" + name
	// actions/checkout stores the tag it checks out as lightweight.
	_, err := git(ctx, sys, "fetch", "--force", "origin", ref+":"+ref)
	if err != nil {
		return "", &toolError{Code: "tag_not_found", Message: err.Error()}
	}

	kind, err := git(ctx, sys, "cat-file", "-t", ref)
	if err != nil {
		return "", err
	}
	if kind != "tag" {
		return "", &toolError{Code: "lightweight_tag", Message: name + " is not an annotated tag"}
	}

	message, err := git(ctx, sys, "for-each-ref", "--format=%(contents)", ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		return "", &toolError{Code: "empty_notes", Message: name + " has a blank message"}
	}

	return message, nil
}
