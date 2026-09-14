package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("blobLink resolves a Markdown export's link to a blob from where the export is written",
	func(path, blob, want string) {
		Expect(blobLink(path, blob)).To(Equal(want))
	},
	Entry("from the file's directory when both paths are relative", "docs/rca.md", "blobs/abc", "../blobs/abc"),
	Entry("from a file in the working directory", "rca.md", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path on stdout", "-", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path when the file's is absolute", "/srv/rca.md", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path when it is absolute", "docs/rca.md", "/srv/blobs/abc", "/srv/blobs/abc"),
)
