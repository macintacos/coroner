package cli

import (
	"errors"
	"fmt"
	"io/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("storeError", func() {
	const dbPath = "/data/coroner.db"

	DescribeTable("gives a database failure its kind and keeps the store's error chain out of the message",
		func(err, sentinel error, kind Kind, chainText string) {
			got := storeError(err, dbPath)

			Expect(got).To(SatisfyAll(HaveField("Kind", kind), MatchError(sentinel)))
			Expect(got.Error()).NotTo(ContainSubstring(chainText))
		},
		Entry("busy",
			fmt.Errorf("open %s: %w: %w", dbPath, store.ErrBusy, errors.New("database is locked (5) (SQLITE_BUSY)")),
			store.ErrBusy, KindDBBusy, "SQLITE_BUSY"),
		Entry("too new",
			fmt.Errorf("open %s: %w (schema 9); upgrade coroner", dbPath, store.ErrDatabaseTooNew),
			store.ErrDatabaseTooNew, KindDBTooNew, "schema 9"),
		Entry("foreign",
			fmt.Errorf("open %s: %w", dbPath, store.ErrForeignDatabase),
			store.ErrForeignDatabase, KindDBUnreadable, "open "+dbPath+":"),
		Entry("permission denied",
			fmt.Errorf("create database directory: %w", &fs.PathError{Op: "mkdir", Path: "/data", Err: fs.ErrPermission}),
			fs.ErrPermission, KindDBUnreadable, "mkdir"),
	)

	DescribeTable("names the database file when the fix is to point at another",
		func(err error) {
			Expect(storeError(err, dbPath)).To(MatchError(ContainSubstring(dbPath)))
		},
		Entry("foreign", store.ErrForeignDatabase),
		Entry("permission denied", &fs.PathError{Op: "mkdir", Path: "/data", Err: fs.ErrPermission}),
	)

	It("leaves an existing backup's path and fix to the store's message, with no hint to contradict it", func() {
		err := fmt.Errorf("open %s: back up before migrating: %w: %s.pre-v2.bak; move it aside and retry",
			dbPath, store.ErrBackupExists, dbPath)

		Expect(storeError(err, dbPath)).To(SatisfyAll(
			HaveField("Kind", KindDBUnreadable), HaveField("Message", err.Error()), HaveField("Hint", BeEmpty())))
	})
})
