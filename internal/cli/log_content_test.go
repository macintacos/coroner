package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

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
	sentinels := []string{sentinelTitle, sentinelText, sentinelBody, sentinelContent, sentinelFilename}

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
		newAttachmentPath := filepath.Join(dir, sentinelFilename+"-new.txt")
		Expect(os.WriteFile(newAttachmentPath, []byte("new "+sentinelContent), 0o600)).To(Succeed())

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
			"{dir}", dir, "{attach}", attachmentPath, "{attach2}", newAttachmentPath, "{annotation}", annotationID, "{attachment}", listed.Attachments[0].ID,
		)
	})

	It("keeps record content out of the lines that create and migrate the database", func() {
		logged := readFile(logPath)
		Expect(logged).To(ContainSubstring(`"msg":"migration applied"`))
		for _, sentinel := range sentinels {
			Expect(logged).NotTo(ContainSubstring(sentinel), "record content reached the diagnostic log")
		}
	})

	resolve := func(args []string) []string {
		resolved := make([]string, 0, len(args)+1)
		for _, arg := range args {
			resolved = append(resolved, placeholders.Replace(arg))
		}
		return resolved
	}

	expectNoRecordContent := func(written string) {
		GinkgoHelper()
		for _, sentinel := range sentinels {
			Expect(written).NotTo(ContainSubstring(sentinel), "record content reached the diagnostic log")
		}
	}

	DescribeTable("logs what it read, and no record content", func(ctx SpecContext, read readCommand) {
		cmd, _, err := cli.NewRoot(ctx, h.app()).Find(read.args)
		Expect(err).NotTo(HaveOccurred())
		before := len(readIfExists(logPath))
		Expect(run(ctx, append(resolve(read.args), "--debug")...)).To(Equal(0), errOut.String())

		written := readFile(logPath)[before:]
		Expect(decodeLines(written)).To(ContainElement(SatisfyAll(
			HaveKeyWithValue("level", "DEBUG"),
			HaveKeyWithValue("msg", "records read"),
			HaveKeyWithValue("command", cmd.CommandPath()),
			HaveKeyWithValue("counts", read.counts),
		)), written)
		expectNoRecordContent(written)
	}, readCommandEntries())

	It("lists every command that only reads records", func(ctx SpecContext) {
		root := cli.NewRoot(ctx, h.app())
		var leaves []string
		cli.WalkCommands(root, func(cmd *cobra.Command) {
			if !cmd.Runnable() || cmd.HasSubCommands() || cmd.Name() == "help" ||
				slices.Contains(cli.WriteVerbs(), cmd.Name()) {
				return
			}
			words := strings.Fields(cmd.CommandPath())[1:]
			if _, off := cli.CommandOf(root, words); off {
				return
			}
			leaves = append(leaves, cmd.CommandPath())
		})

		var listed []string
		for _, read := range readCommands() {
			cmd, _, err := root.Find(read.args)
			Expect(err).NotTo(HaveOccurred())
			listed = append(listed, cmd.CommandPath())
		}
		Expect(listed).To(ConsistOf(leaves))
	})

	DescribeTable("keeps record content out of the diagnostic log",
		func(ctx SpecContext, wantExit int, args ...string) {
			resolved := resolve(args)
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
			expectNoRecordContent(written)
		},
		Entry("timeline export as Markdown into a directory", 0, "timeline", "export", "-t", "inc", "--format", "md", "-o", "{dir}/out/"),
		Entry("timeline export as HTML to a file", 0, "timeline", "export", "-t", "inc", "--format", "html", "-o", "{dir}/inc.html"),
		Entry("timeline edit", 0, "timeline", "edit", "inc", "--title", "edited "+sentinelTitle),
		Entry("timeline delete", 0, "timeline", "delete", "inc", "--yes"),
		Entry("event list filtered by source, tag and text", 0, "event", "list", "-t", "inc", "--source", "pagerduty", "--tag", "db", "--contains", sentinelText),
		Entry("timeline view filtered by text", 0, "timeline", "view", "-t", "inc", "--contains", sentinelText),
		Entry("timeline export filtered by text", 0, "timeline", "export", "-t", "inc", "--format", "jsonl", "--contains", sentinelText),
		Entry("event list between typed times", 0, "event", "list", "-t", "inc", "--since", "2020-01-01", "--until", "now"),
		Entry("event add with an attachment", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--attach", "{attach}"),
		Entry("event add with new attachment content", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--attach", "{attach2}"),
		Entry("event add as a dry run", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--dry-run"),
		Entry("event add at typed times", 0, "event", "add", "another "+sentinelText, "-t", "inc", "--at", "2026-01-02 03:04", "--ended", "2026-01-02 04:00"),
		Entry("event edit", 0, "event", "edit", "inc#1", "--text", "edited "+sentinelText),
		Entry("event edit at a typed time", 0, "event", "edit", "1", "-t", "inc", "--at", "2026-01-02 03:04"),
		Entry("annotation add", 0, "annotation", "add", "another "+sentinelBody, "-t", "inc", "-e", "1", "--kind", "evidence"),
		Entry("annotation edit", 0, "annotation", "edit", "{annotation}", "--body", "edited "+sentinelBody),
		Entry("attachment delete", 0, "attachment", "delete", "{attachment}"),
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

// readCommand is a read command's line, as the read specs run it, and the counts its records
// read line carries against their one timeline, event, annotation and attachment.
type readCommand struct {
	args   []string
	counts map[string]any
}

// readCommands is the command line of each command that only reads records, as the read specs
// run it. A new read command joins this list; a spec fails until it does.
func readCommands() []readCommand {
	return []readCommand{
		{[]string{"annotation", "list", "-t", "inc"}, map[string]any{"annotations": 1.0}},
		{[]string{"attachment", "list", "-t", "inc"}, map[string]any{"attachments": 1.0}},
		{[]string{"attachment", "export", "{attachment}", "-o", "{dir}/export.bin"}, map[string]any{"attachments": 1.0}},
		{[]string{"event", "list", "-t", "inc"}, map[string]any{"events": 1.0}},
		{[]string{"event", "view", "inc#1"}, map[string]any{"links": 0.0, "annotations": 1.0}},
		{[]string{"source", "list"}, map[string]any{"sources": 0.0}},
		{[]string{"status", "-t", "inc"}, map[string]any{"timelines": 1.0}},
		{[]string{"tag", "list"}, map[string]any{"tags": 0.0}},
		{[]string{"timeline", "list"}, map[string]any{"timelines": 1.0}},
		{
			[]string{"timeline", "export", "-t", "inc", "--format", "jsonl"},
			map[string]any{"timelines": 1.0, "events": 1.0, "annotations": 1.0, "attachments": 1.0, "links": 0.0, "sources": 0.0},
		},
		{[]string{"timeline", "view", "-t", "inc"}, map[string]any{"timelines": 1.0, "events": 1.0, "annotations": 1.0, "attachments": 1.0}},
	}
}

// readCommandEntries is readCommands as one table entry each.
func readCommandEntries() []TableEntry {
	var entries []TableEntry
	for _, read := range readCommands() {
		entries = append(entries, Entry(strings.Join(read.args, " "), read))
	}
	return entries
}
