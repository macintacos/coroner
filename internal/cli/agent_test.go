package cli_test

import (
	"bytes"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/skills"
)

var _ = Describe("agent skill", func() {
	It("prints skills/coroner/SKILL.md byte for byte without opening the database", func(ctx SpecContext) {
		var out, errOut bytes.Buffer
		want, err := os.ReadFile("../../skills/coroner/SKILL.md")
		Expect(err).NotTo(HaveOccurred())

		// No OpenStore: calling it would panic, and Run would exit 1.
		code := cli.Run(ctx, cli.App{
			Args:      []string{"agent", "skill"},
			Out:       &out,
			Err:       &errOut,
			LookupEnv: func(string) (string, bool) { return "", false },
			Skill:     skills.Coroner,
		})

		Expect(code).To(Equal(0))
		Expect(errOut.String()).To(BeEmpty())
		Expect(out.Bytes()).To(Equal(want))
	})
})
