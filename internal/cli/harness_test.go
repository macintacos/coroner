package cli_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/clitest"
)

var _ = Describe("harness", func() {
	It("sets every seam of the App it builds", func() {
		Expect(clitest.NilSeams(clitest.Harness{}.App())).To(BeEmpty())
	})
})
