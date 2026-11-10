package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		ids          int
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

	capture := func(ctx context.Context, args ...string) string {
		GinkgoHelper()
		mustRun(ctx, args...)
		return strings.TrimSpace(out.String())
	}

	seed := func(ctx SpecContext) {
		dir := GinkgoT().TempDir()
		ids = 0
		h = harness{home: dir, db: filepath.Join(dir, "coroner.db"), out: &out, errOut: &errOut, newID: countedIDs(&ids)}
		logPath = config.DiagnosticLogPath(h.db)

		attachmentPath := filepath.Join(dir, sentinelFilename+".txt")
		Expect(os.WriteFile(attachmentPath, []byte(sentinelContent), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "existing.bin"), nil, 0o600)).To(Succeed())
		newAttachmentPath := filepath.Join(dir, sentinelFilename+"-new.txt")
		Expect(os.WriteFile(newAttachmentPath, []byte("new "+sentinelContent), 0o600)).To(Succeed())

		timelineID := capture(ctx, "timeline", "create", "inc", "--title", sentinelTitle, "-q")
		eventID := capture(ctx, "event", "add", sentinelText, "-t", "inc", "--attach", attachmentPath, "--source", "pagerduty", "-q")
		mustRun(ctx, "event", "add", "second "+sentinelText, "-t", "inc")
		linkID := capture(ctx, "event", "link", "inc#1", "inc#2", "--kind", "led_to", "-q")
		annotationID := capture(ctx, "annotation", "add", sentinelBody, "-t", "inc", "-e", "1", "--kind", "comment", "-q")
		overlayID := capture(ctx, "overlay", "create", "seeded", "-t", "inc", "-q")
		mustRun(ctx, "source", "list", "--json")
		var sources struct {
			Sources []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"sources"`
		}
		Expect(json.Unmarshal(out.Bytes(), &sources)).To(Succeed(), out.String())
		var sourceID string
		for _, s := range sources.Sources {
			if s.Name == "pagerduty" {
				sourceID = s.ID
			}
		}
		Expect(sourceID).NotTo(BeEmpty())
		mustRun(ctx, "attachment", "list", "-t", "inc", "--json")
		var listed struct {
			Attachments []struct {
				ID string `json:"id"`
			} `json:"attachments"`
		}
		Expect(json.Unmarshal(out.Bytes(), &listed)).To(Succeed(), out.String())
		Expect(listed.Attachments).To(HaveLen(1))
		exportPath := filepath.Join(dir, "inc.jsonl")
		mustRun(ctx, "timeline", "export", "-t", "inc", "--format", "jsonl", "-o", exportPath)
		mustRun(ctx, "timeline", "edit", "inc", "--title", "renamed "+sentinelTitle)
		var events strings.Builder
		for line := range strings.Lines(readFile(exportPath)) {
			if strings.Contains(line, `"type":"event"`) {
				events.WriteString(line)
			}
		}
		Expect(os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events.String()), 0o600)).To(Succeed())

		placeholders = strings.NewReplacer(
			"{dir}", dir, "{attach}", attachmentPath, "{attach2}", newAttachmentPath, "{annotation}", annotationID, "{attachment}", listed.Attachments[0].ID,
			"{timeline}", timelineID, "{event}", eventID, "{link}", linkID, "{source}", sourceID, "{overlay}", overlayID,
		)
	}

	BeforeEach(func(ctx SpecContext) { seed(ctx) })

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

	// A row's key, when set, must carry its id; "{new}" is the id the command's run creates.
	type row struct {
		path            string
		args            []string
		level, msg, key string
		id              string
		fields          []any
	}
	rows := []row{
		{"coroner timeline create", []string{"timeline", "create", "other"}, "INFO", "timeline created", "timeline_id", "{new}", nil},
		{"coroner timeline edit", []string{"timeline", "edit", "inc", "--title", "edited " + sentinelTitle}, "INFO", "timeline edited", "timeline_id", "{timeline}", []any{"title"}},
		{"coroner timeline delete", []string{"timeline", "delete", "inc", "--yes"}, "INFO", "timeline deleted", "timeline_id", "{timeline}", nil},
		{"coroner timeline delete", []string{"timeline", "delete", "inc", "--yes"}, "DEBUG", "overlays changed", "overlay_id", "{overlay}", nil},
		{"coroner timeline use", []string{"timeline", "use", "inc"}, "INFO", "current timeline set", "timeline_id", "{timeline}", nil},
		{"coroner timeline use", []string{"timeline", "use", "--clear"}, "INFO", "current timeline cleared", "", "", nil},
		{"coroner event add", []string{"event", "add", "another " + sentinelText, "-t", "inc"}, "INFO", "event added", "event", "{new}", nil},
		{"coroner event edit", []string{"event", "edit", "inc#1", "--text", "edited " + sentinelText}, "INFO", "event edited", "event", "{event}", []any{"text"}},
		{"coroner event delete", []string{"event", "delete", "inc#1"}, "INFO", "event deleted", "event", "{event}", nil},
		{"coroner event link", []string{"event", "link", "inc#1", "inc#2", "--kind", "same_as"}, "INFO", "link added", "link", "{new}", nil},
		{"coroner event unlink", []string{"event", "unlink", "inc#1", "inc#2", "--kind", "led_to"}, "INFO", "link deleted", "link", "{link}", nil},
		{"coroner annotation add", []string{"annotation", "add", "another " + sentinelBody, "-t", "inc", "-e", "1", "--kind", "evidence"}, "INFO", "annotation added", "annotation", "{new}", nil},
		{"coroner annotation edit", []string{"annotation", "edit", "{annotation}", "--body", "edited " + sentinelBody}, "INFO", "annotation edited", "annotation", "{annotation}", nil},
		{"coroner annotation delete", []string{"annotation", "delete", "{annotation}"}, "INFO", "annotation deleted", "annotation", "{annotation}", nil},
		{"coroner attachment delete", []string{"attachment", "delete", "{attachment}"}, "INFO", "attachment deleted", "attachment", "{attachment}", nil},
		{"coroner overlay create", []string{"overlay", "create", "both", "-t", "inc"}, "INFO", "overlay created", "overlay_id", "{new}", nil},
		{"coroner source edit", []string{"source", "edit", "pagerduty", "--name", "paging"}, "INFO", "source edited", "source_id", "{source}", []any{"name"}},
		{"coroner import", []string{"import", "{dir}/inc.jsonl", "--on-conflict", "replace"}, "DEBUG", "timelines replaced", "timeline_id", "{timeline}", nil},
	}

	It("has a row for every write command", func(ctx SpecContext) {
		var walked []string
		cli.WalkCommands(cli.NewRoot(ctx, cli.App{}), func(cmd *cobra.Command) {
			if cmd.Runnable() && slices.Contains(cli.WriteVerbs(), cmd.Name()) {
				walked = append(walked, cmd.CommandPath())
			}
		})
		var named []string
		for _, r := range rows {
			named = append(named, r.path)
		}
		slices.Sort(walked)
		slices.Sort(named)
		Expect(walked).To(Equal(slices.Compact(named)))
	})

	var writeEntries []TableEntry
	for _, r := range rows {
		for _, dryRun := range []bool{true, false} {
			writeEntries = append(writeEntries, Entry(fmt.Sprintf("%s: %s (dry run %t)", strings.Join(r.args, " "), r.msg, dryRun), r, dryRun))
		}
	}

	DescribeTable("logs the record each write command changes, and keeps its content out", func(ctx SpecContext, r row, dryRun bool) {
		args := resolve(r.args)
		if dryRun {
			args = append(args, "--dry-run")
		}
		wantID := placeholders.Replace(strings.ReplaceAll(r.id, "{new}", countedID(ids+1)))

		before := len(readIfExists(logPath))
		Expect(run(ctx, append(args, "--debug")...)).To(Equal(0), errOut.String())

		written := readFile(logPath)[before:]
		line := SatisfyAll(HaveKeyWithValue("level", r.level), HaveKeyWithValue("msg", r.msg))
		if r.key != "" {
			line = SatisfyAll(line, HaveKeyWithValue(r.key, wantID))
		}
		switch {
		case dryRun:
			line = SatisfyAll(line, HaveKeyWithValue("dry_run", true))
		case r.fields != nil:
			line = SatisfyAll(line, Not(HaveKey("dry_run")), HaveKeyWithValue("fields", r.fields))
		default:
			line = SatisfyAll(line, Not(HaveKey("dry_run")))
		}
		Expect(decodeLines(written)).To(ContainElement(line), written)
		expectNoRecordContent(written)
	}, writeEntries)

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
		func(ctx SpecContext, wantExit int, reaches string, args ...string) {
			resolved := resolve(args)
			before := len(readIfExists(logPath))
			Expect(run(ctx, append(resolved, "--debug")...)).To(Equal(wantExit), errOut.String())

			written := readFile(logPath)[before:]
			Expect(written).To(
				ContainSubstring(`"msg":"command finished"`),
				"no finish line: a command that fails before opening its database writes no log, "+
					"so drop its row rather than loosen this",
			)
			Expect(written).To(ContainSubstring(fmt.Sprintf(`"msg":%q`, reaches)), "the row must reach the line it names")
			if wantExit != 0 {
				Expect(written).To(ContainSubstring(`"msg":"command failed"`))
			}
			expectNoRecordContent(written)
		},
		Entry("timeline export as Markdown into a directory", 0, "file written", "timeline", "export", "-t", "inc", "--format", "md", "-o", "{dir}/out/"),
		Entry("timeline export as HTML to a file", 0, "file written", "timeline", "export", "-t", "inc", "--format", "html", "-o", "{dir}/inc.html"),
		Entry("timeline edit", 0, "timeline edited", "timeline", "edit", "inc", "--title", "edited "+sentinelTitle),
		Entry("timeline delete", 0, "timeline deleted", "timeline", "delete", "inc", "--yes"),
		Entry("event list filtered by source, tag and text", 0, "records read", "event", "list", "-t", "inc", "--source", "pagerduty", "--tag", "db", "--contains", sentinelText),
		Entry("timeline view filtered by text", 0, "records read", "timeline", "view", "-t", "inc", "--contains", sentinelText),
		Entry("timeline export filtered by text", 0, "records read", "timeline", "export", "-t", "inc", "--format", "jsonl", "--contains", sentinelText),
		Entry("event list between typed times", 0, "time input resolved", "event", "list", "-t", "inc", "--since", "2020-01-01", "--until", "now"),
		Entry("event add with an attachment", 0, "attachment added", "event", "add", "another "+sentinelText, "-t", "inc", "--attach", "{attach}"),
		Entry("event add with new attachment content", 0, "blob stored", "event", "add", "another "+sentinelText, "-t", "inc", "--attach", "{attach2}"),
		Entry("event add as a dry run", 0, "event added", "event", "add", "another "+sentinelText, "-t", "inc", "--dry-run"),
		Entry("event add at typed times", 0, "time input resolved", "event", "add", "another "+sentinelText, "-t", "inc", "--at", "2026-01-02 03:04", "--ended", "2026-01-02 04:00"),
		Entry("event edit", 0, "event edited", "event", "edit", "inc#1", "--text", "edited "+sentinelText),
		Entry("event edit at a typed time", 0, "time input resolved", "event", "edit", "1", "-t", "inc", "--at", "2026-01-02 03:04"),
		Entry("annotation add", 0, "annotation added", "annotation", "add", "another "+sentinelBody, "-t", "inc", "-e", "1", "--kind", "evidence"),
		Entry("annotation edit", 0, "annotation edited", "annotation", "edit", "{annotation}", "--body", "edited "+sentinelBody),
		Entry("attachment delete", 0, "attachment deleted", "attachment", "delete", "{attachment}"),
		Entry("timeline create on a taken name", 4, "command failed", "timeline", "create", "inc", "--title", sentinelTitle),
		Entry("timeline edit on a missing timeline", 3, "command failed", "timeline", "edit", "missing", "--title", sentinelTitle),
		Entry("event add to a missing timeline", 3, "command failed", "event", "add", sentinelText, "-t", "missing"),
		Entry("event edit of a missing seq selected by -t", 3, "command failed", "event", "edit", "9", "-t", "inc", "--text", "edited "+sentinelText),
		Entry("event edit of a missing seq at a typed time", 3, "command failed", "event", "edit", "9", "-t", "inc", "--at", "2026-01-02 03:04"),
		Entry("event edit on a missing event", 3, "command failed", "event", "edit", "inc#9", "--text", sentinelText),
		Entry("annotation edit on a missing annotation", 3, "command failed", "annotation", "edit", "0199ffff", "--body", sentinelBody),
		Entry("attachment export over an existing file", 4, "command failed", "attachment", "export", "{attachment}", "-o", "{dir}/existing.bin"),
		Entry("import replacing a changed timeline", 0, "timelines replaced", "import", "{dir}/inc.jsonl", "--on-conflict", "replace"),
		Entry("event add from a file", 0, "events skipped", "event", "add", "-t", "inc", "--from-file", "{dir}/events.jsonl"),
		Entry("import of a conflicting export", 4, "command failed", "import", "{dir}/inc.jsonl"),
	)
})

// readCommand is a read command's line, as the read specs run it, and the counts its records
// read line carries against the seed: one timeline, two events linked once, one annotation,
// attachment and source, and no tags.
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
		{[]string{"event", "list", "-t", "inc"}, map[string]any{"events": 2.0}},
		{[]string{"event", "view", "inc#1"}, map[string]any{"links": 1.0, "annotations": 1.0}},
		{[]string{"source", "list"}, map[string]any{"sources": 1.0}},
		{[]string{"status", "-t", "inc"}, map[string]any{"timelines": 1.0}},
		{[]string{"tag", "list"}, map[string]any{"tags": 0.0}},
		{[]string{"timeline", "list"}, map[string]any{"timelines": 1.0}},
		{
			[]string{"timeline", "export", "-t", "inc", "--format", "jsonl"},
			map[string]any{"timelines": 1.0, "events": 2.0, "annotations": 1.0, "attachments": 1.0, "links": 1.0, "sources": 1.0},
		},
		{[]string{"timeline", "view", "-t", "inc"}, map[string]any{"timelines": 1.0, "events": 2.0, "annotations": 1.0, "attachments": 1.0}},
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
