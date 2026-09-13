package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("event references", func() {
	DescribeTable("parseEventRef reads a seq, a timeline's seq, or an id prefix",
		func(ref string, want eventRef) {
			Expect(parseEventRef(ref)).To(Equal(want))
		},
		Entry("a bare seq, in the selected timeline", "12", eventRef{seq: 12}),
		Entry("a timeline's seq", "mongo-outage#12", eventRef{timeline: "mongo-outage", seq: 12}),
		Entry("a timeline id's seq", "0199a1b2-0000-7000-8000-000000000001#3",
			eventRef{timeline: "0199a1b2-0000-7000-8000-000000000001", seq: 3}),
		Entry("a full id", "0199a1b2-0000-7000-8000-000000000001",
			eventRef{idPrefix: "0199a1b2-0000-7000-8000-000000000001"}),
		Entry("an id prefix with a leading zero, which no seq has", "012", eventRef{idPrefix: "012"}),
		Entry("an uppercase id prefix, lowercased", "0199A1B2", eventRef{idPrefix: "0199a1b2"}),
	)

	DescribeTable("parseEventRef rejects anything else as usage",
		func(ref string) {
			_, err := parseEventRef(ref)

			Expect(err).To(HaveField("Kind", KindUsage))
		},
		Entry("empty", ""),
		Entry("a seq with no timeline", "#12"),
		Entry("a timeline with no seq", "mongo-outage#"),
		Entry("a zero seq", "mongo-outage#0"),
		Entry("a seq with a leading zero", "mongo-outage#012"),
		Entry("a non-hex id", "zz-top"),
	)
})
