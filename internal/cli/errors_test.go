package cli_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = It("exits 1 for a kind the exit-code table does not hold", func() {
	Expect(cli.Kind("not_a_kind").ExitCode()).To(Equal(1))
})
