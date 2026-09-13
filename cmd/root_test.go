package cmd_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/cmd"
)

var _ = Describe("root command", func() {
	It("prints Hello, world!", func() {
		var out bytes.Buffer
		root := cmd.NewRoot()
		root.SetOut(&out)
		root.SetArgs([]string{}) // nil makes cobra parse the test binary's os.Args

		Expect(root.Execute()).To(Succeed())
		Expect(out.String()).To(Equal("Hello, world!\n"))
	})
})
