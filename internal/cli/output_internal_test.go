package cli

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("output.rows", func() {
	It("aligns columns on a terminal", func() {
		var out bytes.Buffer

		Expect(output{out: &out, outTTY: true}.rows([][]string{{"*", "a", "x"}, {"", "long-name", "y"}})).To(Succeed())

		Expect(out.String()).To(Equal("*  a          x\n   long-name  y\n"))
	})
})
