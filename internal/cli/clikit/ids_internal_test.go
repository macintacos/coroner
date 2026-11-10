package clikit

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replayIDs", func() {
	var ids ReplayIDs

	BeforeEach(func() {
		var n byte
		ids = ReplayIDs{Draw: func() (uuid.UUID, error) {
			n++
			return uuid.UUID{n}, nil
		}}
	})

	draw := func(count int) []uuid.UUID {
		GinkgoHelper()
		var drawn []uuid.UUID
		for range count {
			id, err := ids.NewID()
			Expect(err).NotTo(HaveOccurred())
			drawn = append(drawn, id)
		}

		return drawn
	}

	It("hands a re-run the ids its first run drew, in order", func() {
		ids.Rewind()
		first := draw(2)
		ids.Rewind()

		Expect(draw(2)).To(Equal(first))
	})

	It("draws new ids past the first run's", func() {
		ids.Rewind()
		first := draw(1)
		ids.Rewind()

		Expect(draw(2)).To(Equal([]uuid.UUID{first[0], {2}}))
	})
})
