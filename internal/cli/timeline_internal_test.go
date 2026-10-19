package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("the picker's shadowing messages", func() {
	picked := timeline.Timeline{ID: "0199a1b2-0000-7000-8000-000000000001", Name: "mongo-outage"}
	DescribeTable("a notice names CORONER_TIMELINE when it selects a timeline",
		func(env map[string]string, wantNotice bool) {
			notice := shadowNotice(envOf(env))

			if wantNotice {
				Expect(notice).To(ContainSubstring(`CORONER_TIMELINE="db-2"`))
			} else {
				Expect(notice).To(BeEmpty())
			}
		},
		Entry("names the variable and its value", map[string]string{"CORONER_TIMELINE": "db-2"}, true),
		Entry("says nothing without the variable", nil, false),
		Entry("says nothing with an empty variable", map[string]string{"CORONER_TIMELINE": ""}, false),
	)

	DescribeTable("a warning after a pick names CORONER_TIMELINE when it selects another timeline",
		func(env map[string]string, wantWarning bool) {
			warning := shadowWarning(envOf(env), picked)

			if wantWarning {
				Expect(warning).To(ContainSubstring(`CORONER_TIMELINE="db-2"`))
				Expect(warning).To(ContainSubstring("mongo-outage"))
			} else {
				Expect(warning).To(BeEmpty())
			}
		},
		Entry("warns when the variable names another timeline", map[string]string{"CORONER_TIMELINE": "db-2"}, true),
		Entry("stays quiet without the variable", nil, false),
		Entry("stays quiet with an empty variable", map[string]string{"CORONER_TIMELINE": ""}, false),
		Entry("stays quiet when the variable names the picked timeline", map[string]string{"CORONER_TIMELINE": "mongo-outage"}, false),
		Entry("stays quiet when the variable holds the picked timeline's id",
			map[string]string{"CORONER_TIMELINE": "0199a1b2-0000-7000-8000-000000000001"}, false),
	)
})
