package export_test

import (
	"errors"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// problemFields returns the field of each problem err reports.
func problemFields(err error) []string {
	GinkgoHelper()
	verr, ok := errors.AsType[*timeline.ValidationError](err)
	Expect(ok).To(BeTrue(), "want a *timeline.ValidationError, got %v", err)
	var fields []string
	for _, p := range verr.Problems {
		fields = append(fields, p.Field)
	}

	return fields
}

var _ = Describe("Validate's problem fields", func() {
	farFuture := export.Instant(at(1, 0).AddDate(9000, 0, 0))
	DescribeTable("are JSON keys of the record's export type",
		func(err error, record reflect.Type) {
			fields := problemFields(err)
			Expect(fields).NotTo(BeEmpty())
			Expect(jsonKeys(record)).To(ContainElements(fields))
		},
		Entry("a timeline", export.Timeline{Name: "INC 1", Refs: []string{" "}}.Domain().Validate(),
			reflect.TypeFor[export.Timeline]()),
		Entry("the source none", export.Source{Name: "none"}.Domain().Validate(), reflect.TypeFor[export.Source]()),
		Entry("a source outside the grammar", export.Source{Name: "Slack"}.Domain().Validate(),
			reflect.TypeFor[export.Source]()),
		Entry("an event", export.Event{
			OccurredAt: export.Instant(at(3, 14).Add(time.Second)), OccurredOffset: new(90000),
			Precision: timeline.PrecisionMinute, EndedAt: new(instant(1, 0)), Source: new("none"),
			Tags: []string{"Bad Tag"}, Text: " ",
		}.Domain().Validate(), reflect.TypeFor[export.Event]()),
		Entry("an event with an unknown precision and out-of-range years", export.Event{
			OccurredAt: farFuture, Precision: "fortnight", EndedAt: new(farFuture), Source: new("Slack"), Text: "x",
		}.Domain().Validate(), reflect.TypeFor[export.Event]()),
		Entry("an annotation", export.Annotation{Kind: "note", Source: new("none")}.Domain().Validate(),
			reflect.TypeFor[export.Annotation]()),
		Entry("a link", export.Link{FromEventID: eventID, ToEventID: eventID, Kind: "after"}.Domain().Validate(),
			reflect.TypeFor[export.Link]()),
		Entry("an attachment", export.Attachment{SHA256: "XYZ", Size: -1}.Domain().Validate(),
			reflect.TypeFor[export.Attachment]()),
	)
})
