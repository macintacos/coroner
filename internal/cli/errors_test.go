package cli_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = DescribeTable("Kind.ExitCode is the documented exit code for each error kind",
	func(kind cli.Kind, want int) {
		Expect(kind.ExitCode()).To(Equal(want))
	},
	Entry(nil, cli.KindInternal, 1),
	Entry(nil, cli.KindUsage, 2),
	Entry(nil, cli.KindInvalidTime, 2),
	Entry(nil, cli.KindInvalidName, 2),
	Entry(nil, cli.KindTimelineRequired, 2),
	Entry(nil, cli.KindAmbiguousID, 2),
	Entry(nil, cli.KindTimelineNotFound, 3),
	Entry(nil, cli.KindEventNotFound, 3),
	Entry(nil, cli.KindAnnotationNotFound, 3),
	Entry(nil, cli.KindTimelineExists, 4),
	Entry(nil, cli.KindIDConflict, 4),
	Entry(nil, cli.KindDBBusy, 5),
	Entry(nil, cli.KindDBUnreadable, 5),
	Entry(nil, cli.KindDBTooNew, 5),
	Entry(nil, cli.KindInterrupted, 130),
)
