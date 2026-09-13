package cli_test

import (
	"encoding/json"
	"strings"

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
	Entry(nil, cli.KindTimelineExists, 4),
	Entry(nil, cli.KindIDConflict, 4),
	Entry(nil, cli.KindDBBusy, 5),
	Entry(nil, cli.KindDBUnreadable, 5),
	Entry(nil, cli.KindDBTooNew, 5),
	Entry(nil, cli.KindInterrupted, 130),
)

// lastLine returns the final line of output that ends in a newline.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	return lines[len(lines)-1]
}

// decodeEnvelope returns the "error" object of a stderr JSON error envelope line.
func decodeEnvelope(line string) map[string]any {
	var envelope map[string]map[string]any
	ExpectWithOffset(1, json.Unmarshal([]byte(line), &envelope)).To(Succeed(), line)
	ExpectWithOffset(1, envelope).To(HaveLen(1), line)
	return envelope["error"]
}
