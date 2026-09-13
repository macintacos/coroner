// Command docgen writes coroner's CLI reference into docs/reference/cli, one Markdown file
// per command. With --check it lists how that directory drifts from the commands instead,
// and exits 1 on any drift. Run it from the module root.
package main

import (
	"bytes"
	"context"
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

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/macintacos/coroner/internal/cli"
)

const outDir = "docs/reference/cli"

func main() {
	checkOnly := flag.Bool("check", false, "list drift from "+outDir+" and exit 1 on any, writing nothing")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/tools/docgen [--check]")
		os.Exit(2)
	}

	// A zero App is enough: rendering pages runs no command.
	root := cli.NewRoot(context.Background(), cli.App{})

	var err error
	if *checkOnly {
		err = check(root, outDir, os.Stderr)
	} else {
		err = write(root, outDir)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "docgen:", err)
		os.Exit(1)
	}
}

// write replaces dir with the reference, so pages of removed commands go with it.
func write(root *cobra.Command, dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return err
	}

	return generate(root, dir)
}

// check prints an added, changed or removed line to out for each page dir that differs
// from root's reference, and fails if any.
func check(root *cobra.Command, dir string, out io.Writer) error {
	tmp, err := os.MkdirTemp("", "docgen")
	if err != nil {
		return err
	}

	return errors.Join(checkAgainst(root, tmp, dir, out), os.RemoveAll(tmp))
}

// checkAgainst is check's body; check owns tmp so its removal error is joined to the result.
func checkAgainst(root *cobra.Command, tmp, dir string, out io.Writer) error {
	err := generate(root, tmp)
	if err != nil {
		return err
	}

	want, err := readPages(os.DirFS(tmp))
	if err != nil {
		return err
	}

	got, err := readPages(os.DirFS(dir))
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

func generate(root *cobra.Command, dir string) error {
	root.DisableAutoGenTag = true

	return doc.GenMarkdownTreeCustom(root, dir, frontMatter, func(link string) string { return link })
}

// frontMatter titles a page by its command path: coroner_event_add.md is "coroner event add".
func frontMatter(filename string) string {
	title := strings.ReplaceAll(strings.TrimSuffix(filepath.Base(filename), ".md"), "_", " ")
	return "---\ntitle: " + title + "\n---\n\n"
}

// readPages reads the .md files at the top of fsys by name. A missing directory reads as empty.
func readPages(fsys fs.FS) (map[string][]byte, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]byte{}, nil
	}

	if err != nil {
		return nil, err
	}

	pages := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		pages[entry.Name()], err = fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
	}

	return pages, nil
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
