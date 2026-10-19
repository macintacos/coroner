package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("equivalent command", func() {
	DescribeTable("shellLine quotes only the words a POSIX shell would change, and sh reads the args back",
		func(ctx SpecContext, args []string, want string) {
			line := shellLine(args)
			Expect(line).To(Equal(want))
			Expect(shellSplit(ctx, line)).To(Equal(args))
		},
		Entry("bare words", []string{"coroner", "event", "add", "--at=2026-09-11T03:14:07Z"}, "coroner event add --at=2026-09-11T03:14:07Z"),
		Entry("spaces", []string{"Primary stepped down"}, "'Primary stepped down'"),
		Entry("a single quote", []string{"it's down"}, `'it'\''s down'`),
		Entry("a dollar sign", []string{"$HOME"}, "'$HOME'"),
		Entry("a backtick", []string{"`date`"}, "'`date`'"),
		Entry("a newline", []string{"first\nsecond"}, "'first\nsecond'"),
		Entry("an empty string", []string{"--url", ""}, "--url ''"),
		Entry("a leading dash", []string{"--", "-5s"}, "-- -5s"),
	)

	DescribeTable("commandArgs gives the positional argument, then each given flag in definition order",
		func(row commandArgsRow) {
			Expect(commandArgs(row.path, row.def, row.in)).To(Equal(row.want))
		},
		commandArgsEntries(),
	)

	DescribeTable("commandArgs gives a line the command reads back as the same values",
		func(ctx SpecContext, row commandArgsRow) {
			args := commandArgs(row.path, row.def, row.in)
			root := NewRoot(ctx, App{})

			cmd, rest, err := root.Find(args[1:])
			Expect(err).NotTo(HaveOccurred())
			Expect(cmd.CommandPath()).To(Equal("coroner " + row.path))
			Expect(cmd.ParseFlags(rest)).To(Succeed())

			Expect(row.def.read(cmd.Flags(), cmd.Flags().Args())).To(Equal(row.in))
		},
		commandArgsEntries(),
	)
})

// commandArgsRow is a command's values and the args that give them.
type commandArgsRow struct {
	name, path string
	def        definition
	in         fieldValues
	want       []string
}

func commandArgsEntries() []TableEntry {
	rows := []commandArgsRow{
		{
			name: "event add, every field", path: "event add", def: eventAddDefinition(),
			in: fieldValues{
				"timeline": {"mongo-outage"}, "text": {"Deploy started"}, "tz": {"Europe/Berlin"},
				"at": {"2026-09-11 14:00"}, "ended": {"2026-09-11 14:20"}, "approximate": {"true"},
				"source": {"mongod"}, "tag": {"election", "phase:detect"}, "url": {"https://example.com/l/1"},
				"author": {"sre"}, "dry-run": {"true"},
			},
			want: []string{
				"coroner", "event", "add", "Deploy started", "-t", "mongo-outage", "--tz", "Europe/Berlin",
				"--at", "2026-09-11 14:00", "--ended", "2026-09-11 14:20", "--approximate", "--source", "mongod",
				"--tag", "election", "--tag", "phase:detect", "--url", "https://example.com/l/1", "--author", "sre", "--dry-run",
			},
		},
		{
			name: "event add, text with a leading dash", path: "event add", def: eventAddDefinition(),
			in:   fieldValues{"text": {"-5 s of lag"}, "source": {"mongod"}},
			want: []string{"coroner", "event", "add", "--source", "mongod", "--", "-5 s of lag"},
		},
		{
			name: "event add, flag values with a leading dash", path: "event add", def: eventAddDefinition(),
			in:   fieldValues{"text": {"Pager fires"}, "timeline": {"-outage"}, "at": {"-90m"}},
			want: []string{"coroner", "event", "add", "Pager fires", "--timeline=-outage", "--at=-90m"},
		},
		{
			name: "event add, a bool given false", path: "event add", def: eventAddDefinition(),
			in:   fieldValues{"text": {"Pager fires"}, "approximate": {"false"}},
			want: []string{"coroner", "event", "add", "Pager fires", "--approximate=false"},
		},
		{
			name: "event add, no text", path: "event add", def: eventAddDefinition(),
			in:   fieldValues{"source": {"mongod"}},
			want: []string{"coroner", "event", "add", "--source", "mongod"},
		},
		{
			name: "annotation add", path: "annotation add", def: annotationAddDefinition(),
			in: fieldValues{
				"body": {"Step-down lands 5 s after the COLLSCAN burst"}, "kind": {"comment"}, "event": {"12"},
				"timeline": {"mongo-outage"},
			},
			want: []string{
				"coroner", "annotation", "add", "Step-down lands 5 s after the COLLSCAN burst",
				"-t", "mongo-outage", "-e", "12", "--kind", "comment",
			},
		},
	}

	entries := make([]TableEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, Entry(row.name, row))
	}

	return entries
}
