package timeline

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("join", func() {
	It("keeps an error that is not a *ValidationError", func() {
		plain := errors.New("plain")
		Expect(join(problem("text", RuleBlank, "text: must not be blank"), plain)).To(MatchError(plain))
	})
})
