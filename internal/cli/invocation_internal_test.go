package cli

import (
	"context"
	"io/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("invocation", func() {
	// invocationApp is an App on two terminals with env and, when config is not empty, that
	// config file; it fails the spec if anything opens the store.
	invocationApp := func(env map[string]string, config string) App {
		env["HOME"] = "/home/priya"
		return App{
			InIsTTY: true, OutIsTTY: true, ErrIsTTY: true,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: func(string) ([]byte, error) {
				if config == "" {
					return nil, fs.ErrNotExist
				}
				return []byte(config), nil
			},
			Now: testNow,
			OpenStore: func(context.Context, string) (*store.Store, error) {
				Fail("readInvocation opened the store")
				return nil, nil
			},
			OpenStoreReadOnly: func(context.Context, string) (*store.Store, error) {
				Fail("readInvocation opened the store")
				return nil, nil
			},
		}
	}

	read := func(ctx context.Context, app App, args ...string) invocation {
		cmd, rest, err := NewRoot(ctx, app).Find(append([]string{"event", "list"}, args...))
		Expect(err).NotTo(HaveOccurred())
		Expect(cmd.ParseFlags(rest)).To(Succeed())

		inv, err := readInvocation(cmd, app)
		Expect(err).NotTo(HaveOccurred())
		return inv
	}

	DescribeTable("resolves the zone only when asked, from --tz, then default_timezone, then local",
		func(ctx SpecContext, config string, args []string, want string) {
			z, err := read(ctx, invocationApp(map[string]string{}, config), args...).zone()

			Expect(err).NotTo(HaveOccurred())
			Expect(z.label).To(Equal(want))
		},
		Entry("--tz", `default_timezone = "Asia/Tokyo"`, []string{"--tz", "Europe/Berlin"}, "Europe/Berlin"),
		Entry("default_timezone", `default_timezone = "Asia/Tokyo"`, nil, "Asia/Tokyo"),
		Entry("neither", "", nil, "local"),
	)

	It("reads a command line with a bad --tz, and fails only zone()", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, ""), "--tz", "Bogus/Zone")

		_, err := inv.zone()
		Expect(err).To(BeAssignableToTypeOf(&Error{}))
		Expect(err).To(HaveField("Kind", KindUsage))
	})

	DescribeTable("turns input off",
		func(ctx SpecContext, app App, args []string, wantOff, wantTerminal bool) {
			inv := read(ctx, app, args...)

			Expect(inv.inputOff).To(Equal(wantOff))
			Expect(inv.terminal).To(Equal(wantTerminal))
		},
		Entry("neither", invocationApp(map[string]string{}, ""), nil, false, true),
		Entry("--no-input", invocationApp(map[string]string{}, ""), []string{"--no-input"}, true, false),
		Entry("CORONER_NO_INPUT set", invocationApp(map[string]string{"CORONER_NO_INPUT": "1"}, ""), nil, true, false),
		Entry("CORONER_NO_INPUT false", invocationApp(map[string]string{"CORONER_NO_INPUT": "false"}, ""), nil, false, true),
		Entry("--no-input=false over CORONER_NO_INPUT",
			invocationApp(map[string]string{"CORONER_NO_INPUT": "1"}, ""), []string{"--no-input=false"}, false, true),
		Entry("stdin not a terminal", withTTY(invocationApp(map[string]string{}, ""), false, true, true), nil, false, false),
		Entry("stdout not a terminal", withTTY(invocationApp(map[string]string{}, ""), true, false, true), nil, false, false),
		Entry("TERM=dumb", invocationApp(map[string]string{"TERM": "dumb"}, ""), nil, false, false),
	)

	DescribeTable("colours each stream",
		func(ctx SpecContext, app App, args []string, wantOut, wantErr bool) {
			inv := read(ctx, app, args...)

			Expect(inv.outColor).To(Equal(wantOut))
			Expect(inv.errColor).To(Equal(wantErr))
		},
		Entry("both on", invocationApp(map[string]string{}, ""), nil, true, true),
		Entry("--no-color", invocationApp(map[string]string{}, ""), []string{"--no-color"}, false, false),
		Entry("NO_COLOR", invocationApp(map[string]string{"NO_COLOR": "1"}, ""), nil, false, false),
		Entry("CORONER_NO_COLOR", invocationApp(map[string]string{"CORONER_NO_COLOR": "1"}, ""), nil, false, false),
		Entry("TERM=dumb", invocationApp(map[string]string{"TERM": "dumb"}, ""), nil, false, false),
		Entry("stdout not a terminal", withTTY(invocationApp(map[string]string{}, ""), true, false, true), nil, false, true),
		Entry("stderr not a terminal", withTTY(invocationApp(map[string]string{}, ""), true, true, false), nil, true, false),
	)

	It("repeats only the --db and --config the command line gave", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, `default_timezone = "local"`), "--config", "y", "--db", "x")
		Expect(inv.echoArgs).To(Equal([]string{"--db", "x", "--config", "y"}))

		inv = read(ctx, invocationApp(map[string]string{"CORONER_DB": "/tmp/env.db"}, ""))
		Expect(inv.echoArgs).To(BeEmpty())
	})

	It("reads --debug and CORONER_DEBUG from parsed flags", func(ctx SpecContext) {
		Expect(read(ctx, invocationApp(map[string]string{}, "")).debug).To(BeFalse())
		Expect(read(ctx, invocationApp(map[string]string{}, ""), "--debug").debug).To(BeTrue())
		Expect(read(ctx, invocationApp(map[string]string{"CORONER_DEBUG": "1"}, "")).debug).To(BeTrue())
	})

	It("never opens the store", func(ctx SpecContext) {
		inv := read(ctx, invocationApp(map[string]string{}, ""))

		Expect(inv.cfg.DBPath).NotTo(BeEmpty())
	})

	It("lets a viewer open on a terminal until --format is given", func(ctx SpecContext) {
		Expect(read(ctx, invocationApp(map[string]string{}, "")).interactive()).To(BeTrue())
		Expect(read(ctx, invocationApp(map[string]string{}, ""), "--format", "text").interactive()).To(BeFalse())
	})
})

// withTTY returns app with stdin, stdout and stderr on a terminal as given.
func withTTY(app App, in, out, errOut bool) App {
	app.InIsTTY, app.OutIsTTY, app.ErrIsTTY = in, out, errOut
	return app
}
