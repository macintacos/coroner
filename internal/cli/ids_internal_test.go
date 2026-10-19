package cli

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replayIDs", func() {
	var ids replayIDs

	BeforeEach(func() {
		var n byte
		ids = replayIDs{draw: func() (uuid.UUID, error) {
			n++
			return uuid.UUID{n}, nil
		}}
	})

	draw := func(count int) []uuid.UUID {
		GinkgoHelper()
		var drawn []uuid.UUID
		for range count {
			id, err := ids.newID()
			Expect(err).NotTo(HaveOccurred())
			drawn = append(drawn, id)
		}

		return drawn
	}

	It("hands a re-run the ids its first run drew, in order", func() {
		ids.rewind()
		first := draw(2)
		ids.rewind()

		Expect(draw(2)).To(Equal(first))
	})

	It("draws new ids past the first run's", func() {
		ids.rewind()
		first := draw(1)
		ids.rewind()

		Expect(draw(2)).To(Equal([]uuid.UUID{first[0], {2}}))
	})
})
