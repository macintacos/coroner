package timeline_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Attachment.Validate", func() {
	valid := func() timeline.Attachment {
		return timeline.Attachment{
			ID:        "0199a1b2-0000-7000-8000-000000000002",
			Owner:     timeline.AttachmentOwner{EventID: new("0199a1b2-0000-7000-8000-000000000001")},
			Filename:  "grafana.png",
			MIME:      "image/png",
			SHA256:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Size:      0,
			CreatedAt: created(),
		}
	}

	It("accepts an attachment on an event", func() {
		Expect(valid().Validate()).To(Succeed())
	})

	It("accepts an attachment on an annotation", func() {
		a := valid()
		a.Owner = timeline.AttachmentOwner{AnnotationID: new("0199a1b2-0000-7000-8000-000000000003")}
		Expect(a.Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(rule string, mutate func(*timeline.Attachment)) {
			a := valid()
			mutate(&a)
			Expect(a.Validate()).To(MatchError(HavePrefix(rule)))
		},
		Entry("no owner", "owner", func(a *timeline.Attachment) { a.Owner = timeline.AttachmentOwner{} }),
		Entry("both an event and an annotation as owner", "owner", func(a *timeline.Attachment) {
			a.Owner.AnnotationID = new("0199a1b2-0000-7000-8000-000000000003")
		}),
		Entry("a sha256 one digit short", "sha256", func(a *timeline.Attachment) { a.SHA256 = a.SHA256[1:] }),
		Entry("an uppercase sha256", "sha256", func(a *timeline.Attachment) {
			a.SHA256 = "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"
		}),
		Entry("a sha256 that is not hex", "sha256", func(a *timeline.Attachment) {
			a.SHA256 = "g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		}),
		Entry("a negative size", "size", func(a *timeline.Attachment) { a.Size = -1 }),
		Entry("a filename that is not UTF-8", "filename: must be UTF-8", func(a *timeline.Attachment) { a.Filename = "caf\xe9.png" }),
		Entry("a mime that is not UTF-8", "mime: must be UTF-8", func(a *timeline.Attachment) { a.MIME = "image/\xff" }),
	)

	It("reports every broken rule at once", func() {
		a := valid()
		a.Owner = timeline.AttachmentOwner{}
		a.Size = -1
		Expect(a.Validate()).To(And(MatchError(ContainSubstring("owner")), MatchError(ContainSubstring("size"))))
	})
})
