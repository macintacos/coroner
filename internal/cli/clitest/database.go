package clitest

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
)

// ExpectNothingInvalidStored fails unless every timeline in the database at db exports, which
// export refuses when any stored string is not UTF-8, and returns that export as JSONL. run is cli.Run, which this package cannot import.
func ExpectNothingInvalidStored(ctx context.Context, run func(context.Context, clikit.App) int, db string) string {
	GinkgoHelper()
	s, err := store.Open(ctx, db)
	Expect(err).NotTo(HaveOccurred())
	timelines, err := s.Timelines(ctx)
	Expect(errors.Join(err, s.Close())).NotTo(HaveOccurred())
	args := []string{"timeline", "export", "--format", "jsonl"}
	for _, t := range timelines {
		args = append(args, "-t", t.Name)
	}
	var out, errOut strings.Builder
	Expect(run(ctx, Harness{Home: filepath.Dir(db), DB: db, Out: &out, ErrOut: &errOut}.App(args...))).To(Equal(0), errOut.String())
	return out.String()
}

// BumpSchemaVersion records one migration past the highest the database at path applied.
func BumpSchemaVersion(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		"INSERT INTO goose_db_version (version_id, is_applied) SELECT max(version_id) + 1, 1 FROM goose_db_version")
	return errors.Join(err, db.Close())
}

// ReadTree returns the content of every file under dir, by its path relative to dir.
func ReadTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	fsys := os.DirFS(dir)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files[path], err = fs.ReadFile(fsys, path)
		return err
	})

	return files, err
}
