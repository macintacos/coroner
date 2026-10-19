package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// system is how the tool reaches git and gh, injected so specs can script it.
type system struct {
	// output runs a command and returns its stdout, trimmed. The child's stderr goes to the
	// tool's stderr, and a non-zero exit is an error carrying the command and that stderr.
	output func(ctx context.Context, name string, args ...string) (string, error)
	// run runs a command with its stdout and stderr both going to the tool's stderr, keeping
	// the tool's stdout for its one JSON object.
	run func(ctx context.Context, name string, args ...string) error
}

func realSystem(stderr io.Writer) system {
	return system{
		output: func(ctx context.Context, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // runs git, gh and mise with arguments the tool builds
			var errOut bytes.Buffer
			cmd.Stderr = io.MultiWriter(stderr, &errOut)
			out, err := cmd.Output()
			if err != nil {
				return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
			}

			return strings.TrimSpace(string(out)), nil
		},
		run: func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // runs git, gh and mise with arguments the tool builds
			cmd.Stdout, cmd.Stderr = stderr, stderr
			err := cmd.Run()
			if err != nil {
				return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
			}

			return nil
		},
	}
}

func git(ctx context.Context, sys system, args ...string) (string, error) {
	out, err := sys.output(ctx, "git", args...)
	if err != nil {
		return "", &toolError{Code: "git_error", Message: err.Error()}
	}

	return out, nil
}

func gh(ctx context.Context, sys system, args ...string) (string, error) {
	out, err := sys.output(ctx, "gh", args...)
	if err != nil {
		return "", &toolError{Code: "github_error", Message: err.Error()}
	}

	return out, nil
}
