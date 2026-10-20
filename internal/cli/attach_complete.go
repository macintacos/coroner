package cli

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// attachCompletions completes a typed path from the entries of its directory, read through
// readDir: relative to where coroner runs, or absolute. It keeps the entries that start with the
// typed name, leaving out one starting with . until a . is typed, and ends a directory with /.
// Nothing typed, a path starting with ~/, which the form passes on unexpanded so the echoed
// command stays equivalent, and a directory it cannot read all complete nothing.
func attachCompletions(readDir func(string) ([]fs.DirEntry, error)) func(typed string) []string {
	return func(typed string) []string {
		if typed == "" || strings.HasPrefix(typed, "~/") {
			return nil
		}
		dir, name := filepath.Split(typed)
		entries, err := readDir(filepath.Clean(dir))
		if err != nil {
			return nil
		}
		var completions []string
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), name) || strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(name, ".") {
				continue
			}
			completion := dir + entry.Name()
			if entry.IsDir() {
				completion += "/"
			}
			completions = append(completions, completion)
		}

		return completions
	}
}
