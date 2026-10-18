package cli_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
		Cmds:               map[string]func(*testscript.TestScript, bool, []string){"exits": exits},
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

// runCoroner is coroner as scripts run it: main.go's App with the clock pinned.
func runCoroner() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)

	version := "(devel)"
	if build, ok := debug.ReadBuildInfo(); ok {
		version = build.Main.Version
	}
	app, err := withTestClock(cli.App{
		Args:      os.Args[1:],
		In:        os.Stdin,
		Out:       os.Stdout,
		Err:       os.Stderr,
		OutIsTTY:  isatty.IsTerminal(os.Stdout.Fd()),
		ErrIsTTY:  isatty.IsTerminal(os.Stderr.Fd()),
		Version:   version,
		LookupEnv: os.LookupEnv,
		ReadFile:  os.ReadFile,
		Now:       time.Now,
		NewID:     uuid.NewV7,
		OpenStore: store.Open,
	})
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
