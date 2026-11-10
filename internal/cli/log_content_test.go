package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/config"
)

// The record content the diagnostic log must never carry, one sentinel per kind of content.
// Each is plain ASCII, so JSON escaping cannot hide it.
const (
	sentinelTitle    = "SENTINEL-TITLE-q7v2"
	sentinelText     = "SENTINEL-TEXT-q7v2"
	sentinelBody     = "SENTINEL-BODY-q7v2"
	sentinelContent  = "SENTINEL-CONTENT-q7v2"
	sentinelFilename = "SENTINEL-FILENAME-q7v2"
)

var _ = Describe("log content guard", func() {
	var (
		h            harness
		logPath      string
		out, errOut  bytes.Buffer
		placeholders *strings.Replacer
	)

	run := func(ctx context.Context, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, h.app(args...))
	}

	mustRun := func(ctx context.Context, args ...string) {
		GinkgoHelper()
		Expect(run(ctx, args...)).To(Equal(0), errOut.String())
	}

	BeforeEach(func(ctx SpecContext) {
		dir := GinkgoT().TempDir()
		ids := 0
		h = harness{home: dir, db: filepath.Join(dir, "coroner.db"), out: &out, errOut: &errOut, newID: countedIDs(&ids)}
		logPath = config.DiagnosticLogPath(h.db)

		attachmentPath := filepath.Join(dir, sentinelFilename+".txt")
		Expect(os.WriteFile(attachmentPath, []byte(sentinelContent), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "existing.bin"), nil, 0o600)).To(Succeed())

		mustRun(ctx, "timeline", "create", "inc", "--title", sentinelTitle)
		mustRun(ctx, "event", "add", sentinelText, "-t", "inc", "--attach", attachmentPath)
		mustRun(ctx, "annotation", "add", sentinelBody, "-t", "inc", "-e", "1", "--kind", "comment", "-q")
		annotationID := strings.TrimSpace(out.String())
		mustRun(ctx, "attachment", "list", "-t", "inc", "--json")
		var listed struct {
			Attachments []struct {
				ID string `json:"id"`
			} `json:"attachments"`
		}
		Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed(), out.String())
		Expect(listed.Attachments).To(HaveLen(1))

		placeholders = strings.NewReplacer(
			"{dir}", dir, "{attach}", attachmentPath, "{annotation}", annotationID, "{attachment}", listed.Attachments[0].ID,
		)
	})

	DescribeTable("keeps record content out of the diagnostic log",
		func(ctx SpecContext, wantExit int, args ...string) {
			resolved := make([]string, 0, len(args)+1)
			for _, arg := range args {
				resolved = append(resolved, placeholders.Replace(arg))
			}
			before := len(readIfExists(logPath))
			Expect(run(ctx, append(resolved, "--debug")...)).To(Equal(wantExit), errOut.String())

			written := readFile(logPath)[before:]
			Expect(written).To(
				ContainSubstring(`"msg":"command finished"`),
				"no finish line: a command that fails before opening its database writes no log, "+
					"so drop its row rather than loosen this",
			)
			if wantExit != 0 {
				Expect(written).To(ContainSubstring(`"msg":"command failed"`))
			}
			for _, sentinel := range []string{sentinelTitle, sentinelText, sentinelBody, sentinelContent, sentinelFilename} {
				Expect(written).NotTo(ContainSubstring(sentinel), "record content reached the diagnostic log")
			}
		},
		Entry("timeline list", 0, "timeline", "list"),
		Entry("timeline export as JSONL", 0, "timeline", "export", "-t", "inc", "--format", "jsonl"),
		Entry("timeline export as HTML to a file", 0, "timeline", "export", "-t", "inc", "--format", "html", "-o", "{dir}/inc.html"),
		Entry("timeline edit", 0, "timeline", "edit", "inc", "--title", "edited "+sentinelTitle),
		Entry("timeline delete", 0, "timeline", "delete", "inc", "--yes"),
		Entry("event list", 0, "event", "list", "-t", "inc"),
		Entry("event list between typed times", 0, "event", "list", "-t", "inc", "--since", "2020-01-01", "--until", "now"),
		Entry("event view", 0, "event", "view", "inc#1"),
		Entry("event add with an attachment", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--attach", "{attach}"),
		Entry("event add as a dry run", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--dry-run"),
		Entry("event add at typed times", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--at", "2026-01-02 03:04", "--ended", "2026-01-02 04:00"),
		Entry("event edit", 0, "event", "edit", "inc#1", "--text", "edited "+sentinelText),
		Entry("event edit at a typed time", 0, "event", "edit", "1", "-t", "inc", "--at", "2026-01-02 03:04"),
		Entry("annotation list", 0, "annotation", "list", "-t", "inc"),
		Entry("annotation add", 0, "annotation", "add", "another "+sentinelBody, "-t", "inc", "-e", "1", "--kind", "evidence"),
		Entry("annotation edit", 0, "annotation", "edit", "{annotation}", "--body", "edited "+sentinelBody),
		Entry("attachment list", 0, "attachment", "list", "-t", "inc"),
		Entry("attachment export to a file", 0, "attachment", "export", "{attachment}", "-o", "{dir}/export.bin"),
		Entry("timeline create on a taken name", 4, "timeline", "create", "inc", "--title", sentinelTitle),
		Entry("timeline edit on a missing timeline", 3, "timeline", "edit", "missing", "--title", sentinelTitle),
		Entry("event add to a missing timeline", 3, "event", "add", sentinelText, "-t", "missing"),
		Entry("event edit of a missing seq selected by -t", 3, "event", "edit", "9", "-t", "inc", "--text", "edited "+sentinelText),
		Entry("event edit of a missing seq at a typed time", 3, "event", "edit", "9", "-t", "inc", "--at", "2026-01-02 03:04"),
		Entry("event edit on a missing event", 3, "event", "edit", "inc#9", "--text", sentinelText),
		Entry("annotation edit on a missing annotation", 3, "annotation", "edit", "0199ffff", "--body", sentinelBody),
		Entry("attachment export over an existing file", 4, "attachment", "export", "{attachment}", "-o", "{dir}/existing.bin"),
	)
})
