package store_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("buildEventQuery", func() {
	It("passes hostile input only as args, one per placeholder", func() {
		const hostile = "'); DROP TABLE event; --"
		since := time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC)
		f := store.EventFilter{
			Since:    &since,
			Until:    new(since.Add(time.Hour)),
			Sources:  []string{hostile, hostile + "2"},
			Tags:     []string{hostile, hostile + "3"},
			Contains: hostile,
			Order:    store.OrderDesc,
			Limit:    5,
		}

		query, args := store.BuildEventQuery(hostile, f)

		Expect(query).NotTo(ContainSubstring("DROP"))
		Expect(args).To(ContainElements(hostile, hostile+"2", hostile+"3"))
		Expect(strings.Count(query, "?")).To(Equal(len(args)))
		Expect(args).To(HaveLen(9))
	})
})
