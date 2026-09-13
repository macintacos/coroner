package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"maps"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("Run", func() {
	var (
		out, errOut bytes.Buffer
		env, files  map[string]string
	)

	newApp := func(args ...string) cli.App {
		return cli.App{
			Args: args,
			Out:  &out,
			Err:  &errOut,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: func(name string) ([]byte, error) {
				content, ok := files[name]
				if !ok {
					return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
				}

				return []byte(content), nil
			},
		}
	}

	BeforeEach(func() {
		out.Reset()
		errOut.Reset()
		env = map[string]string{"HOME": "/home/ada"}
		files = map[string]string{}
	})

	It("prints Hello, world! to Out and exits 0 without reading the process args", func(ctx SpecContext) {
		DeferCleanup(func(saved []string) { os.Args = saved }, os.Args)
		os.Args = []string{"coroner", "--no-such-flag"}

		code := cli.Run(ctx, newApp())

		Expect(code).To(Equal(0))
		Expect(out.String()).To(Equal("Hello, world!\n"))
	})

	It("reports a rejected flag on Err and exits 2", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("--no-such-flag"))

		Expect(code).To(Equal(2))
		Expect(errOut.String()).To(ContainSubstring("--no-such-flag"))
		Expect(out.String()).To(BeEmpty())
	})

	It("ends a text-mode error with the fix and prints no JSON", func(ctx SpecContext) {
		cli.Run(ctx, newApp("--no-such-flag"))

		Expect(lastLine(errOut.String())).To(ContainSubstring("coroner --help"))
		Expect(errOut.String()).NotTo(ContainSubstring("{"))
	})

	DescribeTable("ends Err with a one-line JSON error envelope under a machine format",
		func(ctx SpecContext, args ...string) {
			code := cli.Run(ctx, newApp(args...))

			Expect(code).To(Equal(2))
			Expect(out.String()).To(BeEmpty())
			envelope := decodeEnvelope(lastLine(errOut.String()))
			Expect(envelope).To(HaveKeyWithValue("kind", "usage"))
			Expect(envelope).To(HaveKeyWithValue("message", ContainSubstring("--no-such-flag")))
			Expect(envelope).To(HaveKey("hint"))
		},
		Entry("--format json", "--format", "json", "--no-such-flag"),
		Entry("--format=jsonl", "--format=jsonl", "--no-such-flag"),
		Entry("--json before the bad flag", "--json", "--no-such-flag"),
		Entry("--json after the bad flag", "--no-such-flag", "--json"),
	)

	It("exits 130 with kind interrupted once the context is cancelled", func(ctx SpecContext) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		code := cli.Run(cancelled, newApp("--json"))

		Expect(code).To(Equal(130))
		Expect(decodeEnvelope(lastLine(errOut.String()))).To(HaveKeyWithValue("kind", "interrupted"))
	})

	Describe("error colour", func() {
		const red = "\x1b[31m"

		It("marks the error in red when Err is a terminal", func(ctx SpecContext) {
			app := newApp("--no-such-flag")
			app.ErrIsTTY = true

			cli.Run(ctx, app)

			Expect(errOut.String()).To(HavePrefix(red))
		})

		DescribeTable("prints no escape codes when colour is off",
			func(ctx SpecContext, isTTY bool, extraEnv map[string]string, args ...string) {
				maps.Copy(env, extraEnv)
				app := newApp(append(args, "--no-such-flag")...)
				app.ErrIsTTY = isTTY

				cli.Run(ctx, app)

				Expect(errOut.String()).NotTo(ContainSubstring("\x1b"))
			},
			Entry("Err is not a terminal", false, nil),
			Entry("NO_COLOR is set", true, map[string]string{"NO_COLOR": "1"}),
			Entry("CORONER_NO_COLOR is set", true, map[string]string{"CORONER_NO_COLOR": "1"}),
			Entry("TERM is dumb", true, map[string]string{"TERM": "dumb"}),
			Entry("--no-color is passed", true, nil, "--no-color"),
		)
	})

	Describe("debug output", func() {
		It("logs the failure's kind and exit code under --debug", func(ctx SpecContext) {
			cli.Run(ctx, newApp("--debug", "--no-such-flag"))

			Expect(errOut.String()).To(MatchRegexp(`level=DEBUG .*kind=usage exit_code=2`))
		})

		It("logs the failure under CORONER_DEBUG", func(ctx SpecContext) {
			env["CORONER_DEBUG"] = "1"

			cli.Run(ctx, newApp("--no-such-flag"))

			Expect(errOut.String()).To(ContainSubstring("level=DEBUG"))
		})

		It("prints no log lines without debug", func(ctx SpecContext) {
			cli.Run(ctx, newApp("--no-such-flag"))

			Expect(errOut.String()).NotTo(ContainSubstring("level="))
		})
	})

	It("rejects an empty --db", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("--db="))

		Expect(code).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring("--db"))
	})

	It("reads the config file named by --config and exits 1 on an unknown key", func(ctx SpecContext) {
		files["/etc/coroner.toml"] = `default_timzone = "UTC"`

		code := cli.Run(ctx, newApp("--config", "/etc/coroner.toml"))

		Expect(code).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring("default_timzone"))
		Expect(out.String()).To(BeEmpty())
	})

	It("exits 1 naming the missing config file CORONER_CONFIG points at", func(ctx SpecContext) {
		env["CORONER_CONFIG"] = "/missing/config.toml"

		code := cli.Run(ctx, newApp())

		Expect(code).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring("/missing/config.toml"))
	})

	It("completes the command line even when the default config file is broken", func(ctx SpecContext) {
		files["/home/ada/.config/coroner/config.toml"] = `default_timzone = "UTC"`

		// Cobra writes completions to os.Stdout, not Out, so only the exit code is observable.
		code := cli.Run(ctx, newApp("__complete", ""))

		Expect(code).To(Equal(0))
	})
})
