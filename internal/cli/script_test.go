package cli_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

func TestMain(m *testing.M) {
	// Registered here and read back with flag.Lookup: gochecknoglobals rejects a package-level flag var.
	flag.Bool("update", false, "rewrite testscript expectations in place")
	testscript.Main(m, map[string]func(){"coroner": runCoroner})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Files:         scriptFiles(),
		UpdateScripts: flag.Lookup("update").Value.String() == "true",
		Setup: func(env *testscript.Env) error {
			env.Setenv("XDG_DATA_HOME", filepath.Join(env.WorkDir, ".local", "share"))
			env.Setenv("XDG_CONFIG_HOME", filepath.Join(env.WorkDir, ".config"))
			env.Setenv("TZ", "America/New_York")
			env.Setenv("CORONER_TEST_NOW", "2026-09-12T21:30:00-04:00")
			return nil
		},
	})
}

// scriptFiles lists scripts rather than passing testscript a Dir, which fails on a directory
// with no scripts; contract/ may have none until real commands exist.
func scriptFiles() []string {
	free, _ := filepath.Glob("testdata/script/*.txtar")
	contract, _ := filepath.Glob("testdata/script/contract/*.txtar")
	return append(free, contract...)
}

// runCoroner is coroner as scripts run it: main.go's App with the clock pinned.
func runCoroner() {
	app, err := withTestClock(cli.App{
		Args:      os.Args[1:],
		In:        os.Stdin,
		Out:       os.Stdout,
		Err:       os.Stderr,
		LookupEnv: os.LookupEnv,
		Now:       time.Now,
		OpenStore: store.Open,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(cli.Run(context.Background(), app))
}

// withTestClock pins app.Now to the RFC 3339 instant in CORONER_TEST_NOW.
func withTestClock(app cli.App) (cli.App, error) {
	value, _ := app.LookupEnv("CORONER_TEST_NOW")
	now, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return app, fmt.Errorf("CORONER_TEST_NOW: %w", err)
	}
	app.Now = func() time.Time { return now }
	return app, nil
}

var _ = Describe("withTestClock", func() {
	lookup := func(value string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			return value, key == "CORONER_TEST_NOW"
		}
	}

	It("pins App.Now to the instant in CORONER_TEST_NOW", func() {
		app, err := withTestClock(cli.App{LookupEnv: lookup("2026-09-12T21:30:00-04:00"), Now: time.Now})

		Expect(err).NotTo(HaveOccurred())
		want := time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)
		Expect(app.Now()).To(BeTemporally("==", want))
		Expect(app.Now()).To(BeTemporally("==", want))
	})

	It("rejects a CORONER_TEST_NOW that is not RFC 3339", func() {
		_, err := withTestClock(cli.App{LookupEnv: lookup("yesterday"), Now: time.Now})

		Expect(err).To(MatchError(ContainSubstring("CORONER_TEST_NOW")))
	})
})
