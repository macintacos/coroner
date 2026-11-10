package internalcmd_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/cli/clitest"
)

var _ = Describe("the __internal group", func() {
	DescribeTable("stays out of root help and the reference topic",
		func(ctx SpecContext, args ...string) {
			var out, errOut bytes.Buffer
			h := clitest.Harness{Home: GinkgoT().TempDir(), Out: &out, ErrOut: &errOut}

			Expect(cli.Run(ctx, h.App(args...))).To(Equal(0), errOut.String())
			Expect(out.String()).To(SatisfyAll(ContainSubstring("event"), Not(ContainSubstring("__internal"))))
		},
		Entry("root help", "--help"),
		Entry("the reference topic", "help", "reference"))
})
