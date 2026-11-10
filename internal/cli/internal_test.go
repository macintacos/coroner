package cli_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("the __internal group", func() {
	DescribeTable("stays out of root help and the reference topic",
		func(ctx SpecContext, args ...string) {
			var out, errOut bytes.Buffer
			h := harness{home: GinkgoT().TempDir(), out: &out, errOut: &errOut}

			Expect(cli.Run(ctx, h.app(args...))).To(Equal(0), errOut.String())
			Expect(out.String()).To(SatisfyAll(ContainSubstring("event"), Not(ContainSubstring("__internal"))))
		},
		Entry("root help", "--help"),
		Entry("the reference topic", "help", "reference"))
})
