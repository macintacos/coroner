package cli

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// attachCompletions completes a typed path, relative to where coroner runs, absolute or under home
// as ~/, to the entries of its directory, read through readDir, a directory ending in /. It matches
// case-sensitively and keeps a typed ~/ because ctrl+e appends the rest of a match to what is
// typed. A name starting with . waits for a typed . and ~/ without a home completes nothing.
// ponytail: reads the directory on every key; cache by directory if a key takes over 100 ms.
func attachCompletions(readDir func(string) ([]fs.DirEntry, error), home string) func(typed string) []string {
	expand := expandHome(home)
	return func(typed string) []string {
		path := expand(typed)
		if path == "" || strings.HasPrefix(path, "~/") {
			return nil
		}
		completions := pathCompletions(readDir, path)
		if path != typed {
			for i, completion := range completions {
				completions[i] = "~" + strings.TrimPrefix(completion, strings.TrimSuffix(home, "/"))
			}
		}

		return completions
	}
}

// expandHome returns what turns a typed path starting with ~/ into the same path under home, and
// leaves any other path, or every path when home is "", as typed.
func expandHome(home string) func(typed string) string {
	return func(typed string) string {
		rest, found := strings.CutPrefix(typed, "~/")
		if !found || home == "" {
			return typed
		}

		return strings.TrimSuffix(home, "/") + "/" + rest
	}
}

// pathCompletions returns the entries of path's directory, read through readDir, that start with
// its last element, a directory ending in /.
func pathCompletions(readDir func(string) ([]fs.DirEntry, error), path string) []string {
	dir, typedName := filepath.Split(path)
	entries, err := readDir(filepath.Clean(dir))
	if err != nil {
		return nil
	}
	var completions []string
	for _, entry := range entries {
		hidden := strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(typedName, ".")
		if hidden || !strings.HasPrefix(entry.Name(), typedName) {
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

// isDir reports whether entry, at path, is a directory, following a symlink.
func isDir(readDir func(string) ([]fs.DirEntry, error), path string, entry fs.DirEntry) bool {
	if entry.Type()&fs.ModeSymlink == 0 {
		return entry.IsDir()
	}
	_, err := readDir(path)

	return err == nil
}
