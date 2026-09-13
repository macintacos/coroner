package cli_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("Run", func() {
	var (
		out, errOut bytes.Buffer
		env         map[string]string
	)

	newApp := func(args ...string) cli.App {
		return cli.App{
			Args:    args,
			Out:     &out,
			Err:     &errOut,
			Version: "v1.2.3",
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
		}
	}

	BeforeEach(func() {
		out.Reset()
		errOut.Reset()
		env = map[string]string{"HOME": "/home/ada"}
	})

	It("runs the command line in App.Args, never the process args", func(ctx SpecContext) {
		DeferCleanup(func(saved []string) { os.Args = saved }, os.Args)
		os.Args = []string{"coroner", "--no-such-flag"}

		code := cli.Run(ctx, newApp("version"))

		Expect(code).To(Equal(0))
		Expect(out.String()).To(Equal("coroner version v1.2.3\n"))
	})

	Describe("version", func() {
		It("prints compact JSON when Out is not a terminal", func(ctx SpecContext) {
			code := cli.Run(ctx, newApp("version", "--format", "json"))

			Expect(code).To(Equal(0))
			Expect(out.String()).To(Equal(`{"coroner_version":"v1.2.3"}` + "\n"))
		})

		It("indents JSON when Out is a terminal", func(ctx SpecContext) {
			app := newApp("version", "--json")
			app.OutIsTTY = true

			cli.Run(ctx, app)

			Expect(out.String()).To(Equal("{\n  \"coroner_version\": \"v1.2.3\"\n}\n"))
		})

		It("is also printed by --version", func(ctx SpecContext) {
			code := cli.Run(ctx, newApp("--version", "--json"))

			Expect(code).To(Equal(0))
			Expect(out.String()).To(Equal(`{"coroner_version":"v1.2.3"}` + "\n"))
		})

		It("exits 1 with kind internal and a way to report it when Out cannot be written", func(ctx SpecContext) {
			app := newApp("version", "--json")
			app.Out = failingWriter{}

			code := cli.Run(ctx, app)

			Expect(code).To(Equal(1))
			envelope := decodeEnvelope(lastLine(errOut.String()))
			Expect(envelope).To(HaveKeyWithValue("kind", "internal"))
			Expect(envelope).To(HaveKeyWithValue("hint", SatisfyAll(
				ContainSubstring("--debug"), ContainSubstring("https://github.com/macintacos/coroner/issues"))))
		})
	})

	DescribeTable("accepts a global flag before or after the subcommand",
		func(ctx SpecContext, flags ...string) {
			Expect(cli.Run(ctx, newApp(append(flags, "version")...))).To(Equal(0))
			before := out.String()
			out.Reset()

			Expect(cli.Run(ctx, newApp(append([]string{"version"}, flags...)...))).To(Equal(0))
			Expect(out.String()).To(Equal(before))
		},
		Entry(nil, "--json"),
		Entry(nil, "--format", "json"),
		Entry(nil, "-q"),
		Entry(nil, "--quiet"),
		Entry(nil, "--no-color"),
		Entry(nil, "--tz", "UTC"),
		Entry(nil, "--debug"),
		Entry(nil, "--db", "/tmp/coroner.db"),
		Entry(nil, "--config", "/etc/coroner.toml"),
	)

	DescribeTable("rejects an output format the command cannot produce with exit 2 and kind usage",
		func(ctx SpecContext, args ...string) {
			code := cli.Run(ctx, newApp(append(args, "--json")...))

			Expect(code).To(Equal(2))
			Expect(out.String()).To(BeEmpty())
			Expect(decodeEnvelope(lastLine(errOut.String()))).To(HaveKeyWithValue("kind", "usage"))
		},
		Entry("--json with --format text", "version", "--format", "text"),
		Entry("--json with --format jsonl", "version", "--format", "jsonl"),
		Entry("an unknown format", "version", "--format", "yaml"),
	)

	It("rejects jsonl for version, which prints a single object", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("version", "--format", "jsonl"))

		Expect(code).To(Equal(2))
		Expect(out.String()).To(BeEmpty())
	})

	It("suggests the nearest command for an unknown one and exits 2", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("versio"))

		Expect(code).To(Equal(2))
		Expect(errOut.String()).To(MatchRegexp(`(?s)Did you mean this\?\s+version`))
	})

	Describe("help", func() {
		It("lays out root help on Out", func(ctx SpecContext) {
			code := cli.Run(ctx, newApp("--help"))

			Expect(code).To(Equal(0))
			Expect(errOut.String()).To(BeEmpty())
			Expect(out.String()).To(MatchRegexp(
				`(?s)^.+\nUSAGE\n.*\nADDITIONAL COMMANDS\n.*version.*\nEXAMPLES\n.*\nFLAGS\n.*--version.*` +
					`\nGLOBAL FLAGS\n.*--db.*\nLEARN MORE\n.*https://github.com/macintacos/coroner/issues`))
		})

		It("names each global flag's environment variable", func(ctx SpecContext) {
			cli.Run(ctx, newApp("--help"))

			Expect(out.String()).To(MatchRegexp(`--db .*CORONER_DB`))
			Expect(out.String()).To(MatchRegexp(`--debug .*CORONER_DEBUG`))
			Expect(out.String()).To(MatchRegexp(`--no-color .*NO_COLOR`))
		})

		It("lists a subcommand's inherited flags under GLOBAL FLAGS", func(ctx SpecContext) {
			cli.Run(ctx, newApp("version", "--help"))

			Expect(out.String()).To(MatchRegexp(`(?s)\nFLAGS\n.*-h, --help.*\nGLOBAL FLAGS\n.*--db`))
		})

		It("is printed when coroner runs without arguments", func(ctx SpecContext) {
			code := cli.Run(ctx, newApp())

			Expect(code).To(Equal(0))
			Expect(out.String()).To(ContainSubstring("USAGE"))
		})
	})

	It("writes a shell completion script to Out", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("completion", "bash"))

		Expect(code).To(Equal(0))
		Expect(out.String()).To(ContainSubstring("bash completion"))
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

		It("logs an internal failure under --debug", func(ctx SpecContext) {
			app := newApp("--debug", "version")
			app.Out = failingWriter{}

			cli.Run(ctx, app)

			Expect(errOut.String()).To(MatchRegexp(`level=DEBUG .*command="coroner version" kind=internal exit_code=1`))
		})
	})
})

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken pipe")
}
