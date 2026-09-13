package cli_test

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"
	"testing"
	"time"

	// Embeds the zone database so TZ resolves on machines without system tzdata instead of silently becoming UTC.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/mattn/go-isatty"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

const updateFlag = "update"

func TestMain(m *testing.M) {
	// Registered here and read back with flag.Lookup: gochecknoglobals rejects a package-level flag var.
	flag.Bool(updateFlag, false, "rewrite testscript expectations in place")
	testscript.Main(m, map[string]func(){"coroner": runCoroner})
}

func TestScripts(t *testing.T) {
	params := testscript.Params{
		UpdateScripts:      flag.Lookup(updateFlag).Value.String() == "true",
		RequireUniqueNames: true,
		Setup:              hermeticEnv,
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"exits": exits, "bumpschema": bumpSchema,
		},
	}
	for _, dir := range []string{"testdata/script", "testdata/script/contract"} {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			files, _ := filepath.Glob(filepath.Join(dir, "*.txtar"))
			if files == nil {
				t.Skip("no scripts in " + dir)
			}
			p := params
			p.Files = files
			testscript.Run(t, p)
		})
	}
}

func hermeticEnv(env *testscript.Env) error {
	env.Setenv("XDG_DATA_HOME", filepath.Join(env.WorkDir, ".local", "share"))
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(env.WorkDir, ".config"))
	// A DST zone, and an evening instant whose UTC date is the next day, so local/UTC mix-ups fail.
	env.Setenv("TZ", "America/New_York")
	env.Setenv("CORONER_TEST_NOW", "2026-09-12T21:30:00-04:00")
	env.Setenv("CORONER_TEST_IDS", filepath.Join(env.WorkDir, ".coroner-test-ids"))
	return nil
}

// exits is `exits <code> <program> [args...]`: exec that also asserts the exact exit code,
// which `! exec` cannot. stdout and stderr stay available to the next assertions.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits <code> <program> [args...]")
	}

	want, err := strconv.Atoi(args[0])
	ts.Check(err)

	got := 0
	err = ts.Exec(args[1], args[2:]...)
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		got = exitErr.ExitCode()
	} else {
		ts.Check(err)
	}

	if got != want {
		ts.Fatalf("%s exited %d, want %d", args[1], got, want)
	}
}

// bumpSchema is `bumpschema <db>`: it records one migration past the highest db applied, as a
// newer coroner would. On a database coroner just wrote, that is past every embedded one.
func bumpSchema(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: bumpschema <db>")
	}

	db, err := sql.Open("sqlite", ts.MkAbs(args[0]))
	ts.Check(err)
	_, err = db.ExecContext(context.Background(),
		"INSERT INTO goose_db_version (version_id, is_applied) SELECT max(version_id) + 1, 1 FROM goose_db_version")
	ts.Check(errors.Join(err, db.Close()))
}

// runCoroner is coroner as scripts run it: main.go's App with the clock and IDs pinned.
func runCoroner() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)

	version := "(devel)"
	if build, ok := debug.ReadBuildInfo(); ok {
		version = build.Main.Version
	}
	app, err := withTestClock(cli.App{
		Args:              os.Args[1:],
		In:                os.Stdin,
		Out:               os.Stdout,
		Err:               os.Stderr,
		InIsTTY:           isatty.IsTerminal(os.Stdin.Fd()),
		OutIsTTY:          isatty.IsTerminal(os.Stdout.Fd()),
		ErrIsTTY:          isatty.IsTerminal(os.Stderr.Fd()),
		Version:           version,
		LookupEnv:         os.LookupEnv,
		ReadFile:          os.ReadFile,
		Now:               time.Now,
		NewID:             uuid.NewV7,
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	})
	if err == nil {
		app, err = withTestIDs(app)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		// Distinct from coroner's exit codes, so an exact exit-code assertion tells a broken harness from a coroner failure.
		os.Exit(125)
	}
	os.Exit(cli.Run(ctx, app))
}

func withTestClock(app cli.App) (cli.App, error) {
	value, _ := app.LookupEnv("CORONER_TEST_NOW")
	now, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return app, fmt.Errorf("CORONER_TEST_NOW: %w", err)
	}
	app.Now = now.Local
	return app, nil
}

var _ = Describe("withTestClock", func() {
	fakeLookupEnv := func(value string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			return value, key == "CORONER_TEST_NOW"
		}
	}

	It("pins App.Now to the instant in CORONER_TEST_NOW", func() {
		app, err := withTestClock(cli.App{LookupEnv: fakeLookupEnv("2026-09-12T21:30:00-04:00"), Now: time.Now})

		Expect(err).NotTo(HaveOccurred())
		want := time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)
		Expect(app.Now()).To(BeTemporally("==", want))
	})

	It("returns the pinned instant in the local zone, as time.Now does", func() {
		app, err := withTestClock(cli.App{LookupEnv: fakeLookupEnv("2026-09-12T21:30:00+13:07"), Now: time.Now})

		Expect(err).NotTo(HaveOccurred())
		Expect(app.Now().Location().String()).To(Equal(time.Local.String()))
	})

	It("rejects a CORONER_TEST_NOW that is not RFC 3339", func() {
		_, err := withTestClock(cli.App{LookupEnv: fakeLookupEnv("yesterday"), Now: time.Now})

		Expect(err).To(MatchError(ContainSubstring("CORONER_TEST_NOW")))
	})
})

// withTestIDs numbers app's IDs from the counter file CORONER_TEST_IDS names, so the coroner
// processes of one script continue a single sequence.
// ponytail: the counter file is unlocked, so a script must not run coroner in the background.
func withTestIDs(app cli.App) (cli.App, error) {
	path, _ := app.LookupEnv("CORONER_TEST_IDS")
	if path == "" {
		return app, errors.New("CORONER_TEST_IDS is not set")
	}

	millis := app.Now().UnixMilli()
	readFile := app.ReadFile
	app.NewID = func() (uuid.UUID, error) {
		n, err := nextCount(readFile, path)
		return testID(millis, n), err
	}
	return app, nil
}

// nextCount increments the count stored in the file at path, which starts at 0 when missing.
func nextCount(readFile func(string) ([]byte, error), path string) (uint64, error) {
	data, err := readFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}

	n, err := strconv.ParseUint(cmp.Or(string(data), "0"), 10, 64)
	if err != nil {
		return 0, err
	}
	n++
	return n, os.WriteFile(path, []byte(strconv.FormatUint(n, 10)), 0o600)
}

// testID returns the UUIDv7 whose timestamp is millis and whose random bits hold n.
func testID(millis int64, n uint64) uuid.UUID {
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[:8], uint64(max(millis, 0))<<16)
	binary.BigEndian.PutUint64(id[8:], n)
	id[6] = 0x70
	id[8] = id[8]&0x3f | 0x80
	return id
}

var _ = Describe("withTestIDs", func() {
	pinned := func() time.Time { return time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC) }
	fakeLookupEnv := func(value string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			return value, key == "CORONER_TEST_IDS"
		}
	}

	It("gives UUIDv7 IDs stamped with App.Now's millisecond and numbered from 1", func() {
		app, err := withTestIDs(cli.App{
			LookupEnv: fakeLookupEnv(filepath.Join(GinkgoT().TempDir(), "ids")), Now: pinned, ReadFile: os.ReadFile, NewID: uuid.NewV7,
		})
		Expect(err).NotTo(HaveOccurred())

		first, err := app.NewID()
		Expect(err).NotTo(HaveOccurred())
		second, err := app.NewID()
		Expect(err).NotTo(HaveOccurred())

		Expect(first.String()).To(Equal("01a09862-b1c0-7000-8000-000000000001"))
		Expect(second.String()).To(Equal("01a09862-b1c0-7000-8000-000000000002"))
		Expect(first.Version()).To(Equal(uuid.Version(7)))
		Expect(first.Variant()).To(Equal(uuid.RFC4122))
	})

	It("continues the numbering across apps sharing CORONER_TEST_IDS, as a script's coroner processes do", func() {
		lookupEnv := fakeLookupEnv(filepath.Join(GinkgoT().TempDir(), "ids"))
		earlier, err := withTestIDs(cli.App{LookupEnv: lookupEnv, Now: pinned, ReadFile: os.ReadFile, NewID: uuid.NewV7})
		Expect(err).NotTo(HaveOccurred())
		_, err = earlier.NewID()
		Expect(err).NotTo(HaveOccurred())

		later, err := withTestIDs(cli.App{LookupEnv: lookupEnv, Now: pinned, ReadFile: os.ReadFile, NewID: uuid.NewV7})
		Expect(err).NotTo(HaveOccurred())
		id, err := later.NewID()

		Expect(err).NotTo(HaveOccurred())
		Expect(id.String()).To(Equal("01a09862-b1c0-7000-8000-000000000002"))
	})

	It("rejects an unset CORONER_TEST_IDS", func() {
		_, err := withTestIDs(cli.App{LookupEnv: fakeLookupEnv(""), Now: pinned, ReadFile: os.ReadFile, NewID: uuid.NewV7})

		Expect(err).To(MatchError(ContainSubstring("CORONER_TEST_IDS")))
	})
})
