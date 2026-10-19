package export_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
)

var _ = DescribeTable("AttachmentPath names the file by its hash under attachments/, with the extension of its media type",
	func(mime, want string) {
		hash := strings.Repeat("3f", 32)

		Expect(export.AttachmentPath(export.Attachment{SHA256: hash, MIME: mime})).To(Equal("attachments/" + hash + want))
	},
	Entry("a PNG", "image/png", ".png"),
	Entry("a JPEG", "image/jpeg", ".jpg"),
	Entry("an SVG, as text a browser will not run", "image/svg+xml", ".txt"),
	Entry("a PDF", "application/pdf", ".pdf"),
	Entry("HTML, as text a browser will not run", "text/html; charset=utf-8", ".txt"),
	Entry("XML, as text a browser will not run", "text/xml; charset=utf-8", ".txt"),
	Entry("text with a charset parameter", "text/plain; charset=utf-8", ".txt"),
	Entry("a type it has no extension for", "application/x-coroner-unknown", ".bin"),
)
