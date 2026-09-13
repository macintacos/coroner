package cli_test

import (
	"bytes"
	"io/fs"
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

	It("reports a rejected flag on Err and exits 1", func(ctx SpecContext) {
		code := cli.Run(ctx, newApp("--no-such-flag"))

		Expect(code).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring("--no-such-flag"))
		Expect(out.String()).To(BeEmpty())
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
})
