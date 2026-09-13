package export_test

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
)

func encode(v any) (string, error) {
	var b bytes.Buffer
	err := export.Write(&b, v)

	return b.String(), err
}

var _ = Describe("Write", func() {
	DescribeTable("emits every key of a zero-valued object, null for a missing scalar and [] for a collection",
		func(v any, want string) {
			Expect(encode(v)).To(Equal(want + "\n"))
		},
		Entry("timeline", export.Timeline{},
			`{"id":"","name":"","title":"","refs":[],"created_at":"0001-01-01T00:00:00.000000000Z"}`),
		Entry("source", export.Source{},
			`{"id":"","name":"","created_at":"0001-01-01T00:00:00.000000000Z"}`),
		Entry("event", export.Event{},
			`{"id":"","timeline_id":"","seq":null,"occurred_at":"0001-01-01T00:00:00.000000000Z","occurred_offset":null,`+
				`"precision":"","approximate":false,"occurred_raw":null,"ended_at":null,"source":null,"tags":[],"text":"",`+
				`"url":null,"author":null,"created_at":"0001-01-01T00:00:00.000000000Z","updated_at":"0001-01-01T00:00:00.000000000Z"}`),
		Entry("annotation", export.Annotation{},
			`{"id":"","timeline_id":"","event_id":null,"kind":"","body":"","author":null,"source":null,"url":null,`+
				`"created_at":"0001-01-01T00:00:00.000000000Z","updated_at":"0001-01-01T00:00:00.000000000Z"}`),
	)

	It("emits a new envelope's format identifiers, its version and time, an empty query and empty record lists", func() {
		envelope := export.NewEnvelope("0.1.0", time.Date(2026, 9, 13, 1, 30, 0, 0, time.FixedZone("", 3600)))

		Expect(encode(envelope)).To(Equal(
			`{"format":"coroner.export","format_version":"1.0","coroner_version":"0.1.0","exported_at":"2026-09-13T00:30:00.000000000Z",` +
				`"query":{"timelines":[],"since":null,"until":null,"sources":[],"tags":[],"contains":null,"limit":null},` +
				`"timelines":[],"sources":[],"events":[],"annotations":[]}` + "\n"))
	})

	It("sorts map keys", func() {
		m := map[string]int{}
		pairs := make([]string, 26)
		for i := range pairs {
			key := string(rune('a' + i))
			m[key] = i
			pairs[i] = fmt.Sprintf("%q:%d", key, i)
		}

		Expect(encode(m)).To(Equal("{" + strings.Join(pairs, ",") + "}\n"))
	})

	It("escapes <, > and & so the output can sit inside an HTML script element", func() {
		Expect(encode(export.Source{Name: "</script>&"})).To(ContainSubstring(`"name":"\u003c/script\u003e\u0026"`))
	})

	It("fails on invalid UTF-8 rather than replacing it, and writes nothing", func() {
		// The long name makes a streaming encoder flush before it reaches the invalid title.
		out, err := encode(export.Timeline{Name: strings.Repeat("a", 1<<16), Title: "\xff"})

		Expect(err).To(MatchError(ContainSubstring("invalid UTF-8")))
		Expect(out).To(BeEmpty())
	})
})

var _ = Describe("Instant", func() {
	DescribeTable("encodes as fixed-width UTC with nine fractional digits",
		func(t time.Time, want string) {
			Expect(encode(export.Instant(t))).To(Equal(`"` + want + `"` + "\n"))
		},
		Entry("a whole second", time.Date(2026, 9, 12, 14, 5, 3, 0, time.UTC), "2026-09-12T14:05:03.000000000Z"),
		Entry("a millisecond in +05:30", time.Date(2026, 9, 12, 19, 35, 3, 180_000_000, time.FixedZone("IST", 19800)),
			"2026-09-12T14:05:03.180000000Z"),
		Entry("the zero value", time.Time{}, "0001-01-01T00:00:00.000000000Z"),
	)

	DescribeTable("fails for a year the fixed width cannot hold",
		func(t time.Time, year string) {
			_, err := encode(export.Instant(t))

			Expect(err).To(MatchError(ContainSubstring("year " + year)))
		},
		Entry("after 9999", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "10000"),
		Entry("before 0000", time.Date(-1, 12, 31, 23, 59, 59, 0, time.UTC), "-1"),
	)
})
