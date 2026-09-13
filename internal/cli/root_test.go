package cli_test

import (
	"bytes"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("Run", func() {
	var out, errOut bytes.Buffer

	BeforeEach(func() {
		out.Reset()
		errOut.Reset()
	})

	It("prints Hello, world! to Out and exits 0 without reading the process args", func(ctx SpecContext) {
		DeferCleanup(func(saved []string) { os.Args = saved }, os.Args)
		os.Args = []string{"coroner", "--no-such-flag"}

		code := cli.Run(ctx, cli.App{Out: &out, Err: &errOut})

		Expect(code).To(Equal(0))
		Expect(out.String()).To(Equal("Hello, world!\n"))
	})

	It("reports a rejected flag on Err and exits 1", func(ctx SpecContext) {
		code := cli.Run(ctx, cli.App{Args: []string{"--no-such-flag"}, Out: &out, Err: &errOut})

		Expect(code).To(Equal(1))
		Expect(errOut.String()).To(ContainSubstring("--no-such-flag"))
		Expect(out.String()).To(BeEmpty())
	})
})
