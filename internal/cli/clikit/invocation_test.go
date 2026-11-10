package clikit_test

import (
	"context"
	"io/fs"
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/clitest"
	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("invocation", func() {
	// invocationApp is an App with stdin, stdout and stderr on terminals, env and, when configFile
	// is not empty, that config file; it fails the spec if anything opens the store.
	invocationApp := func(env map[string]string, configFile string) clikit.App {
		env = maps.Clone(env)
		env["HOME"] = "/home/priya"
		return clikit.App{
			InIsTTY: true, OutIsTTY: true, ErrIsTTY: true,
			LookupEnv: clitest.EnvOf(env),
			ReadFile: func(string) ([]byte, error) {
				if configFile == "" {
					return nil, fs.ErrNotExist
				}
				return []byte(configFile), nil
			},
			Now: clitest.TestNow,
			OpenStore: func(context.Context, string, ...store.Option) (*store.Store, error) {
				Fail("readInvocation opened the store")
				return nil, nil
			},
			OpenStoreReadOnly: func(context.Context, string, ...store.Option) (*store.Store, error) {
				Fail("readInvocation opened the store")
				return nil, nil
			},
		}
	}

	read := func(ctx context.Context, app clikit.App, args ...string) clikit.Invocation {
		cmd, rest, err := cli.NewRoot(ctx, app).Find(append([]string{"event", "list"}, args...))
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.ParseFlags(rest)).To(Succeed())

		inv, err := clikit.ReadInvocation(cmd, app)
		Expect(err).NotTo(HaveOccurred())
		return inv
	}

	DescribeTable("resolves the zone only when asked, from --tz, then default_timezone, then UTC",
		func(ctx SpecContext, configFile string, args []string, want string) {
			z, err := read(ctx, invocationApp(map[string]string{}, configFile), args...).Zone()

			Expect(err).NotTo(HaveOccurred())
			Expect(z.Label).To(Equal(want))
		},
		Entry("--tz", `default_timezone = "Asia/Tokyo"`, []string{"--tz", "Europe/Berlin"}, "Europe/Berlin"),
		Entry("default_timezone", `default_timezone = "Asia/Tokyo"`, nil, "Asia/Tokyo"),
		Entry("neither", "", nil, "UTC"),
	)

	It("reads the timeline input with the command's rule", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, ""), "-t", "alpha", "-t", "beta")

		Expect(inv.Timelines).To(Equal(clikit.TimelineInput{
			Names: []string{"alpha", "beta"},
			Rule:  clikit.TimelineRule{Command: "coroner event list", Several: true},
		}))
	})

	It("reads a command line with a bad --tz, and fails only zone()", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, ""), "--tz", "Bogus/Zone")

		_, err := inv.Zone()
		Expect(err).To(BeAssignableToTypeOf(&clikit.Error{}))
		Expect(err).To(HaveField("Kind", clikit.KindUsage))
	})

	It("reads a config with a bad default_timezone, and fails only zone()", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, `default_timezone = "Bogus/Zone"`))

		_, err := inv.Zone()
		Expect(err).To(BeAssignableToTypeOf(&clikit.Error{}))
		Expect(err).To(HaveField("Kind", clikit.KindUsage))
	})

	DescribeTable("turns input off, and finds a terminal only when stdin and stdout are one, TERM can draw and input is on",
		func(ctx SpecContext, app clikit.App, args []string, wantOff, wantTerminal bool) {
			inv := read(ctx, app, args...)

			Expect(inv.InputOff).To(Equal(wantOff))
			Expect(inv.Terminal).To(Equal(wantTerminal))
		},
		Entry("neither", invocationApp(map[string]string{}, ""), nil, false, true),
		Entry("--no-input", invocationApp(map[string]string{}, ""), []string{"--no-input"}, true, false),
		Entry("CORONER_NO_INPUT set", invocationApp(map[string]string{"CORONER_NO_INPUT": "1"}, ""), nil, true, false),
		Entry("CORONER_NO_INPUT false", invocationApp(map[string]string{"CORONER_NO_INPUT": "false"}, ""), nil, false, true),
		Entry("--no-input=false over CORONER_NO_INPUT",
			invocationApp(map[string]string{"CORONER_NO_INPUT": "1"}, ""), []string{"--no-input=false"}, false, true),
		Entry("stdin not a terminal", clitest.WithTTY(invocationApp(map[string]string{}, ""), false, true, true), nil, false, false),
		Entry("stdout not a terminal", clitest.WithTTY(invocationApp(map[string]string{}, ""), true, false, true), nil, false, false),
		Entry("TERM=dumb", invocationApp(map[string]string{"TERM": "dumb"}, ""), nil, false, false),
	)

	DescribeTable("colours each stream",
		func(ctx SpecContext, app clikit.App, args []string, wantOut, wantErr bool) {
			inv := read(ctx, app, args...)

			Expect(inv.OutColor).To(Equal(wantOut))
			Expect(inv.ErrColor).To(Equal(wantErr))
		},
		Entry("both on", invocationApp(map[string]string{}, ""), nil, true, true),
		Entry("--no-color", invocationApp(map[string]string{}, ""), []string{"--no-color"}, false, false),
		Entry("NO_COLOR", invocationApp(map[string]string{"NO_COLOR": "1"}, ""), nil, false, false),
		Entry("CORONER_NO_COLOR", invocationApp(map[string]string{"CORONER_NO_COLOR": "1"}, ""), nil, false, false),
		Entry("TERM=dumb", invocationApp(map[string]string{"TERM": "dumb"}, ""), nil, false, false),
		Entry("stdout not a terminal", clitest.WithTTY(invocationApp(map[string]string{}, ""), true, false, true), nil, false, true),
		Entry("stderr not a terminal", clitest.WithTTY(invocationApp(map[string]string{}, ""), true, true, false), nil, true, false),
	)

	It("repeats only the --db and --config the command line gave", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, `default_timezone = "local"`), "--config", "y", "--db", "x")
		Expect(inv.EchoArgs).To(Equal([]string{"--db", "x", "--config", "y"}))

		inv = read(ctx, invocationApp(map[string]string{"CORONER_DB": "/tmp/env.db"}, ""))
		Expect(inv.EchoArgs).To(BeEmpty())
	})

	It("colours both streams under --no-color=false, whatever NO_COLOR says", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{"NO_COLOR": "1", "CORONER_NO_COLOR": "1"}, ""), "--no-color=false")

		Expect([]bool{inv.OutColor, inv.ErrColor}).To(Equal([]bool{true, true}))
	})

	It("never opens the store", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, ""))

		Expect(inv.Cfg.DBPath).NotTo(BeEmpty())
	})

	It("lets a viewer open on a terminal until --format or --json is given", func(ctx SpecContext) {
		Expect(read(ctx, invocationApp(map[string]string{}, "")).Interactive()).To(BeTrue())
		Expect(read(ctx, invocationApp(map[string]string{}, ""), "--format", "text").Interactive()).To(BeFalse())
		Expect(read(ctx, invocationApp(map[string]string{}, ""), "--json").Interactive()).To(BeFalse())
	})
})
