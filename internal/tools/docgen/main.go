// Command docgen writes coroner's CLI reference into docs/reference/cli, one Markdown file
// per command. With --check it lists how that directory drifts from the commands instead,
// and exits 1 on any drift.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra/doc"

	"github.com/macintacos/coroner/internal/cli"
)

const outDir = "docs/reference/cli"

func main() {
	checkOnly := flag.Bool("check", false, "list drift from "+outDir+" and exit 1 on any, writing nothing")
	flag.Parse()

	var err error
	if *checkOnly {
		err = check(outDir, os.Stderr)
	} else {
		err = write(outDir)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "docgen:", err)
		os.Exit(1)
	}
}

// write replaces dir with the reference, so pages of removed commands go with it.
func write(dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return err
	}

	return generate(dir)
}

// check writes a line to out for each page dir lacks, holds with other content, or holds for
// no command, and fails when there is any.
func check(dir string, out io.Writer) error {
	tmp, err := os.MkdirTemp("", "docgen")
	if err != nil {
		return err
	}

	return errors.Join(checkAgainst(tmp, dir, out), os.RemoveAll(tmp))
}

// checkAgainst is check, generating the reference to compare dir with into tmp.
func checkAgainst(tmp, dir string, out io.Writer) error {
	err := generate(tmp)
	if err != nil {
		return err
	}

	want, err := readTree(os.DirFS(tmp))
	if err != nil {
		return err
	}

	got, err := readTree(os.DirFS(dir))
	if err != nil {
		return err
	}

	lines := drift(dir, want, got)
	if len(lines) == 0 {
		return nil
	}

	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))

	return errors.Join(err, fmt.Errorf("%s is out of date; run `go run ./internal/tools/docgen`", dir))
}

func generate(dir string) error {
	// Building the tree reads nothing from App; only running a command does.
	root := cli.NewRoot(cli.App{})
	root.DisableAutoGenTag = true

	return doc.GenMarkdownTreeCustom(root, dir, frontMatter, func(link string) string { return link })
}

// frontMatter titles a page by its command path: coroner_event_add.md is "coroner event add".
func frontMatter(filename string) string {
	title := strings.ReplaceAll(strings.TrimSuffix(filepath.Base(filename), ".md"), "_", " ")
	return "---\ntitle: " + title + "\n---\n\n"
}

// readTree reads the files at the top of fsys by name. A missing directory reads as empty.
func readTree(fsys fs.FS) (map[string][]byte, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]byte{}, nil
	}

	if err != nil {
		return nil, err
	}

	tree := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		tree[entry.Name()], err = fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
	}

	return tree, nil
}

// drift describes, sorted by path, how got differs from want: "added" for a page only want
// has, "changed" for different content, "removed" for a page only got has.
func drift(dir string, want, got map[string][]byte) []string {
	both := maps.Clone(got)
	maps.Copy(both, want)

	var lines []string
	for _, name := range slices.Sorted(maps.Keys(both)) {
		wantPage, inWant := want[name]
		gotPage, inGot := got[name]

		switch {
		case !inGot:
			lines = append(lines, "added: "+filepath.Join(dir, name))
		case !inWant:
			lines = append(lines, "removed: "+filepath.Join(dir, name))
		case !bytes.Equal(wantPage, gotPage):
			lines = append(lines, "changed: "+filepath.Join(dir, name))
		}
	}

	return lines
}
