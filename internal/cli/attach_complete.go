package cli

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// attachCompletions completes a typed path, relative to where coroner runs or absolute, to the
// entries of its directory, read through readDir, a directory ending in /. It matches
// case-sensitively because ctrl+e appends the rest of a match to what is typed. A name starting
// with . waits for a typed . and ~/ completes nothing, since the form passes it on unexpanded.
// ponytail: reads the directory on every key; cache by directory if a key takes over 100 ms.
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
			hidden := strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(name, ".")
			if hidden || !strings.HasPrefix(entry.Name(), name) {
				continue
			}
			completion := dir + entry.Name()
			if isDir(readDir, filepath.Join(dir, entry.Name()), entry) {
				completion += "/"
			}
			completions = append(completions, completion)
		}

		return completions
	}
}

// isDir reports whether entry, at path, is a directory, following a symlink.
func isDir(readDir func(string) ([]fs.DirEntry, error), path string, entry fs.DirEntry) bool {
	if entry.Type()&fs.ModeSymlink == 0 {
		return entry.IsDir()
	}
	_, err := readDir(path)

	return err == nil
}
