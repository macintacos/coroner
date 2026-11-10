package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/huh/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/cli/annotation"
	"github.com/macintacos/coroner/internal/cli/attachment"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/event"
	"github.com/macintacos/coroner/internal/cli/overlay"
	"github.com/macintacos/coroner/internal/cli/source"
	timelinecmd "github.com/macintacos/coroner/internal/cli/timeline"
	"github.com/macintacos/coroner/internal/config"
)

// writeDefinitions returns every write command's definition, by its path below coroner.
func writeDefinitions() map[string]clikit.Definition {
	return map[string]clikit.Definition{
		"timeline create": timelinecmd.CreateDefinition(),
		"timeline edit":   timelinecmd.EditDefinition(),
		"timeline delete": timelinecmd.DeleteDefinition(),
		"timeline use":    timelinecmd.UseDefinition(),
		"event add":       event.AddDefinition(),
		"event edit":      event.EditDefinition(),
		"event delete":    event.DeleteDefinition(),
		"event link":      event.LinkDefinition(),
		"event unlink":    event.UnlinkDefinition(),

		"annotation add":    annotation.AddDefinition(),
		"annotation edit":   annotation.EditDefinition(),
		"annotation delete": annotation.DeleteDefinition(),

		"attachment delete": attachment.DeleteDefinition(),

		"overlay create": overlay.CreateDefinition(),

		"source edit": source.EditDefinition(),
	}
}

// formExemptFlags are the output and process globals a form never asks for, and --interactive,
// which opens the form. tz changes what is written only where a command reads typed times, and
// those definitions declare it.
func formExemptFlags() []string {
	return []string{"db", "config", "format", "json", "quiet", "no-color", "no-input", "debug", "help", "tz", "interactive"}
}

// commandFormExemptFlags are the flags one write command declares outside its definition, by the
// command's path. event add's --from-file reads every field from a file's records, so no form asks
// for it.
func commandFormExemptFlags() map[string][]string {
	return map[string][]string{"event add": {"from-file"}}
}

// definitionExempt are the write commands that declare no definition, by path, with the reason.
func definitionExempt() map[string]string {
	return map[string]string{"import": "reads a file of records, so it declares plain flags and no definition"}
}

var _ = Describe("write command definitions", func() {
	It("give every command a write verb names a definition, or an exemption with its reason", func(ctx SpecContext) {
		var missing []string
		walkCommands(NewRoot(ctx, clikit.App{}), func(cmd *cobra.Command) {
			path := strings.TrimPrefix(cmd.CommandPath(), "coroner ")
			_, defined := writeDefinitions()[path]
			if slices.Contains(writeVerbs(), cmd.Name()) && !defined && definitionExempt()[path] == "" {
				missing = append(missing, cmd.CommandPath())
			}
		})

		Expect(missing).To(BeEmpty())
	})

	It("give every flag a form field with its help and default, and every form field a flag", func(ctx SpecContext) {
		root := NewRoot(ctx, clikit.App{})

		var problems []string
		for path, def := range writeDefinitions() {
			problems = append(problems, parityProblems(root, path, def)...)
		}

		Expect(problems).To(BeEmpty())
	})

	It("accept every optional form field's zero value, which a form reads as not given, and reject a required one's", func() {
		var problems []string
		for path, def := range writeDefinitions() {
			for i, formField := range def.FormFields(formCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}) {
				formField.Blur()
				err := formField.Error()
				switch required := def.Fields[i].Required; {
				case required && err == nil:
					problems = append(problems, fmt.Sprintf("coroner %s: required form field %s accepts its zero value", path, formField.GetKey()))
				case !required && err != nil:
					problems = append(problems, fmt.Sprintf("coroner %s: form field %s rejects its zero value: %v", path, formField.GetKey(), err))
				}
			}
		}

		Expect(problems).To(BeEmpty())
	})

	It("return usage by flag for each required field left out", func(ctx SpecContext) {
		given := map[string]string{"text": "Pager fires", "body": "Election timeout", "kind": "comment"}
		for path, def := range writeDefinitions() {
			for _, missing := range def.Fields {
				if !missing.Required {
					continue
				}
				args := strings.Fields(path)
				for _, f := range def.Fields {
					if f.Required && f.Name != missing.Name {
						Expect(given).To(HaveKey(f.Name), "no value to give coroner %s's required field", path)
						args = append(args, fieldArg(f, given[f.Name]))
					}
				}

				Expect(flagError(ctx, args)).To(HaveField("Kind", clikit.KindUsage), "coroner %s", strings.Join(args, " "))
			}
		}
	})

	DescribeTable("reject a bad value the same way by flag and by form field",
		func(ctx SpecContext, bad badValue) {
			def, ok := writeDefinitions()[bad.path]
			Expect(ok).To(BeTrue(), "no definition for %q", bad.path)

			byFlag := flagError(ctx, commandLine(def, bad))
			byForm := formError(def, bad.field, bad.value)

			Expect(byForm).To(SatisfyAll(
				HaveField("Kind", byFlag.Kind), HaveField("Message", byFlag.Message), HaveField("Hint", byFlag.Hint)))
		},
		badValueEntries(),
	)

	It("have a bad value for every checked field", func() {
		covered := map[string]bool{}
		for _, bad := range badValues() {
			covered[bad.path+" "+bad.field] = true
		}

		var missing []string
		for path, def := range writeDefinitions() {
			for _, f := range def.Fields {
				// -t's and --add-ref's checks reject only a blank value, which a form reads as not given.
				// --title's rejects only bytes that are not UTF-8, which a form's input replaces.
				exempt := (f.Name == "timeline" && !f.Arg) || f.Name == "add-ref" ||
					path+" "+f.Name == "timeline create title" || path+" "+f.Name == "timeline edit title"
				if f.Check != nil && !exempt && !covered[path+" "+f.Name] {
					missing = append(missing, path+" "+f.Name)
				}
			}
		}

		Expect(missing).To(BeEmpty())
	})

	It("mark every field of an edit command that is not its argument, an inherited flag or --dry-run as a change", func() {
		var unmarked []string
		for path, def := range writeDefinitions() {
			if !strings.HasSuffix(path, " edit") {
				continue
			}
			for _, f := range def.Fields {
				if !f.Arg && !f.Inherited && f.Name != "dry-run" && !f.Changes {
					unmarked = append(unmarked, "coroner "+path+" --"+f.Name)
				}
			}
		}

		Expect(unmarked).To(BeEmpty())
	})
})

var _ = Describe("definition", func() {
	def := clikit.Definition{Fields: []clikit.Field{
		{Name: "target", Arg: true},
		{Name: "title"},
		{Name: "ref", Typ: clikit.StringsField},
		{Name: "yes", Typ: clikit.BoolField},
		{Name: "untouched"},
	}}

	It("reads the arguments and only the flags given, keeping a repeatable flag's lone empty value", func() {
		flags := pflag.NewFlagSet("throwaway", pflag.ContinueOnError)
		def.AddFlags(flags)
		Expect(flags.Parse([]string{"--ref", "", "--yes=false", "--title", "Primary election storm"})).To(Succeed())

		Expect(def.Read(flags, []string{"12", "14"})).To(Equal(clikit.FieldValues{
			"target": {"12", "14"}, "title": {"Primary election storm"}, "ref": {""}, "yes": {"false"},
		}))
	})

	It("tells a boolean given false from one not given", func() {
		in := clikit.FieldValues{"yes": {"false"}}

		value, given := in.Boolean("yes")
		Expect(value).To(BeFalse())
		Expect(given).To(BeTrue())
		_, given = in.Boolean("untouched")
		Expect(given).To(BeFalse())
	})

	It("checks every value of a repeatable field, by flag and by form line", func() {
		rejectBad := clikit.WithoutEnv(func(tag string) error {
			if tag == "Bad Tag" {
				return errors.New("bad tag")
			}
			return nil
		})
		tags := clikit.Definition{Fields: []clikit.Field{{Name: "tag", Typ: clikit.StringsField, Check: rejectBad}}}

		Expect(tags.Check(clikit.FieldValues{"tag": {"election", "Bad Tag"}}, clikit.CheckEnv{})).To(MatchError("bad tag"))
		Expect(formError(tags, "tag", "election\nBad Tag")).To(MatchError("bad tag"))
		Expect(formError(tags, "tag", "")).To(Succeed())
	})

	edit := clikit.Definition{Fields: []clikit.Field{
		{Name: "id", Arg: true},
		{Name: "body", Changes: true},
		{Name: "author"},
		{Name: "kind", Changes: true},
		{Name: "url", Changes: true},
	}}

	It("refuses no change, naming the changing flags in field order", func() {
		in := clikit.FieldValues{"id": {"0199a1b2"}, "author": {"priya"}}

		Expect(edit.Changed(in)).To(BeFalse())
		failure, ok := errors.AsType[*clikit.Error](edit.RequireChange(in))
		Expect(ok).To(BeTrue())
		Expect(failure).To(SatisfyAll(
			HaveField("Kind", clikit.KindUsage), HaveField("Message", "nothing to edit"), HaveField("Hint", "Pass --body, --kind or --url.")))
	})

	It("accepts any one change", func() {
		for _, name := range []string{"body", "kind", "url"} {
			in := clikit.FieldValues{"id": {"0199a1b2"}, name: {"x"}}

			Expect(edit.Changed(in)).To(BeTrue(), name)
			Expect(edit.RequireChange(in)).To(Succeed(), name)
		}
	})
})

var _ = Describe("write run functions", func() {
	DescribeTable("return usage, not a panic, when given no value for the positional argument their command requires",
		func(ctx SpecContext, run func(context.Context, clikit.App, config.Config, clikit.FieldValues) error, in clikit.FieldValues) {
			err := runWithoutStore(ctx, run, in)

			failure, ok := errors.AsType[*clikit.Error](err)
			Expect(ok).To(BeTrue(), "returned %v", err)
			Expect(failure.Kind).To(Equal(clikit.KindUsage))
		},
		Entry("timeline create", errorOf(timelinecmd.Create), clikit.FieldValues{}),
		Entry("timeline edit", errorOf(timelinecmd.Edit), clikit.FieldValues{"title": {"Primary election storm"}}),
		Entry("timeline delete", errorOf(timelinecmd.Delete), clikit.FieldValues{"yes": {"true"}}),
		Entry("timeline use", errorOf(timelinecmd.Use), clikit.FieldValues{}),
		Entry("event edit", errorOf(event.Edit), clikit.FieldValues{"author": {"sre"}}),
		Entry("event delete", errorOf(event.Delete), clikit.FieldValues{}),
		Entry("event link", errorOf(event.Link), clikit.FieldValues{"kind": {"led_to"}}),
		Entry("event unlink", errorOf(event.Unlink), clikit.FieldValues{"kind": {"led_to"}}),
		Entry("annotation edit", errorOf(annotation.Edit), clikit.FieldValues{"kind": {"comment"}}),
		Entry("annotation delete", errorOf(annotation.Delete), clikit.FieldValues{}),
		Entry("attachment delete", errorOf(attachment.Delete), clikit.FieldValues{}),
		Entry("overlay create", errorOf(overlay.Create), clikit.FieldValues{"timeline": {"mongo-failover"}}),
		Entry("source edit", errorOf(source.Edit), clikit.FieldValues{"name": {"mongodb"}}),
	)

	DescribeTable("return nothing to edit, naming every changing flag, when an edit is given no change",
		func(ctx SpecContext, run func(context.Context, clikit.App, config.Config, clikit.FieldValues) error, in clikit.FieldValues, hint string) {
			err := runWithoutStore(ctx, run, in)

			failure, ok := errors.AsType[*clikit.Error](err)
			Expect(ok).To(BeTrue(), "returned %v", err)
			Expect(failure).To(SatisfyAll(
				HaveField("Kind", clikit.KindUsage), HaveField("Message", "nothing to edit"), HaveField("Hint", hint)))
		},
		Entry("event edit", errorOf(event.Edit), clikit.FieldValues{"event": {"12"}},
			"Pass --text, --at, --ended, --approximate, --source, --add-tag, --remove-tag, --url, --author or --attach."),
		Entry("annotation edit", errorOf(annotation.Edit), clikit.FieldValues{"id": {"0199a1b2"}},
			"Pass --body, --kind, --source, --url or --attach."),
		Entry("timeline edit", errorOf(timelinecmd.Edit), clikit.FieldValues{"timeline": {"mongo-outage"}},
			"Pass --name, --title, --add-ref or --remove-ref."),
		Entry("source edit", errorOf(source.Edit), clikit.FieldValues{"source": {"mongo"}}, "Pass --name."),
	)
})

// runWithoutStore calls run with an App whose store fails the spec when opened.
func runWithoutStore(ctx context.Context, run func(context.Context, clikit.App, config.Config, clikit.FieldValues) error, in clikit.FieldValues) error {
	return run(ctx, storelessApp(nil), config.Config{DBPath: "/nonexistent/coroner.db", DefaultTimezone: "local"}, in)
}

// errorOf returns run with its result dropped.
func errorOf[R any](
	run func(context.Context, clikit.App, config.Config, clikit.FieldValues) (R, error),
) func(context.Context, clikit.App, config.Config, clikit.FieldValues) error {
	return func(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) error {
		_, err := run(ctx, app, cfg, in)
		return err
	}
}

// parityProblems names each flag of the command at path with no matching form field, each form
// field with no flag, each field declared twice, and how a matching pair differs.
func parityProblems(root *cobra.Command, path string, def clikit.Definition) []string {
	cmd, _, err := root.Find(strings.Fields(path))
	if err != nil || cmd.CommandPath() != "coroner "+path {
		return []string{"coroner " + path + ": no such command"}
	}

	problems := duplicateFieldProblems(path, def)
	form := map[string]huh.Field{}
	declared := map[string]clikit.Field{}
	for i, formField := range def.FormFields(clikit.CheckEnv{}, clikit.FieldValues{}, clikit.FormChoices{}) {
		// A positional argument's form field never stands for a flag.
		if !def.Fields[i].Arg {
			form[formField.GetKey()] = formField
			declared[def.Fields[i].Name] = def.Fields[i]
		}
	}
	flags := pflag.NewFlagSet(path, pflag.ContinueOnError)
	flags.AddFlagSet(cmd.LocalFlags())
	flags.AddFlagSet(cmd.InheritedFlags())

	flags.VisitAll(func(flag *pflag.Flag) {
		formField, ok := form[flag.Name]
		switch {
		case !ok && !slices.Contains(formExemptFlags(), flag.Name) && !slices.Contains(commandFormExemptFlags()[path], flag.Name):
			problems = append(problems, fmt.Sprintf("coroner %s: flag --%s has no form field", path, flag.Name))
		case ok:
			problems = append(problems, fieldProblems(path, flag, formField)...)
			problems = append(problems, flagShapeProblems(path, flag, declared[flag.Name])...)
		}
	})
	for _, f := range def.Fields {
		if !f.Arg && flags.Lookup(f.Name) == nil {
			problems = append(problems, fmt.Sprintf("coroner %s: form field %s has no flag", path, f.Name))
		}
	}

	return problems
}

// duplicateFieldProblems names each field def declares more than once.
func duplicateFieldProblems(path string, def clikit.Definition) []string {
	var problems []string
	seen := map[string]bool{}
	for _, f := range def.Fields {
		if seen[f.Name] {
			problems = append(problems, fmt.Sprintf("coroner %s: field %s is declared twice", path, f.Name))
		}
		seen[f.Name] = true
	}

	return problems
}

// flagShapeProblems names how flag's type or shorthand differs from the field f declares.
func flagShapeProblems(path string, flag *pflag.Flag, f clikit.Field) []string {
	var problems []string
	flagType := map[clikit.FieldType]string{clikit.StringField: "string", clikit.BoolField: "bool", clikit.StringsField: "stringArray"}[f.Typ]
	if flag.Value.Type() != flagType {
		problems = append(problems, fmt.Sprintf("coroner %s: field %s is a %s, flag --%s a %s",
			path, f.Name, flagType, flag.Name, flag.Value.Type()))
	}
	if flag.Shorthand != f.Shorthand {
		problems = append(problems, fmt.Sprintf("coroner %s: field %s has shorthand %q, flag --%s %q",
			path, f.Name, f.Shorthand, flag.Name, flag.Shorthand))
	}

	return problems
}

// fieldProblems names how field's help text or default differs from flag's.
func fieldProblems(path string, flag *pflag.Flag, formField huh.Field) []string {
	var problems []string
	_, usage := pflag.UnquoteUsage(flag)
	if !strings.Contains(fieldView(formField), usage) {
		problems = append(problems, fmt.Sprintf("coroner %s: form field %s does not show %q", path, flag.Name, usage))
	}

	// A repeatable flag's default [] is a Text field's empty string, and a list field's no values.
	flagDefault := strings.TrimSuffix(strings.TrimPrefix(flag.DefValue, "["), "]")
	if formDefault := strings.TrimSuffix(strings.TrimPrefix(fmt.Sprint(formField.GetValue()), "["), "]"); formDefault != flagDefault {
		problems = append(problems, fmt.Sprintf("coroner %s: form field %s defaults to %q, flag --%s to %q",
			path, flag.Name, formDefault, flag.Name, flag.DefValue))
	}

	return problems
}

// badValue is a value the field of the write command at path rejects, and the other arguments
// that command needs to reach the field's check.
type badValue struct {
	path, field, value string
	otherArgs          []string
}

// badValues are the rows of the validation table.
func badValues() []badValue {
	return []badValue{
		{"timeline create", "name", "Mongo Outage", nil},
		{"timeline create", "ref", "INC-\xff", []string{"mongo-outage"}},
		{"timeline edit", "name", "Bad Name", []string{"mongo-outage"}},
		{"event add", "text", " ", nil},
		{"event add", "tz", "Mars/Olympus", []string{"Pager fires"}},
		{"event add", "at", "2026-13-45", []string{"Pager fires"}},
		{"event add", "ended", "~2026-09-11T03:14:00Z", []string{"Pager fires"}},
		{"event add", "source", "DB 2", []string{"Pager fires"}},
		{"event add", "tag", "Bad Tag", []string{"Pager fires"}},
		{"event add", "url", " ", []string{"Pager fires"}},
		{"event add", "author", " ", []string{"Pager fires"}},
		{"event add", "attach", "-", []string{"Pager fires"}},
		{"event add", "id", "3b241101-e2bb-4255-8caf-4136c566a962", []string{"Pager fires"}},
		{"event edit", "event", "Bad Ref!", []string{"--dry-run"}},
		{"event edit", "text", " ", []string{"12"}},
		{"event edit", "at", "2026-13-45", []string{"12"}},
		{"event edit", "ended", "~2026-09-11T03:14:00Z", []string{"12"}},
		{"event edit", "source", " ", []string{"12"}},
		{"event edit", "add-tag", "Bad Tag", []string{"12"}},
		{"event edit", "url", " ", []string{"12"}},
		{"event edit", "author", " ", []string{"12"}},
		{"event edit", "tz", "Mars/Olympus", []string{"12"}},
		{"event edit", "attach", "-", []string{"12"}},
		{"event delete", "events", "Bad Ref!", nil},
		{"event link", "events", "Bad Ref!", []string{"--kind", "led_to", "12"}},
		{"event link", "kind", "caused_by", []string{"12", "14"}},
		{"event link", "author", " ", []string{"--kind", "led_to", "12", "14"}},
		{"event unlink", "events", "Bad Ref!", []string{"--kind", "led_to", "12"}},
		{"event unlink", "kind", "caused_by", []string{"12", "14"}},
		{"annotation add", "body", " ", []string{"--kind", "comment"}},
		{"annotation add", "event", "Bad Ref!", []string{"--kind", "comment", "Election timeout"}},
		{"annotation add", "kind", "note", []string{"Election timeout"}},
		{"annotation add", "source", "DB 2", []string{"--kind", "comment", "Election timeout"}},
		{"annotation add", "url", " ", []string{"--kind", "comment", "Election timeout"}},
		{"annotation add", "author", " ", []string{"--kind", "comment", "Election timeout"}},
		{"annotation add", "attach", "-", []string{"--kind", "comment", "Election timeout"}},
		{"annotation edit", "id", "Bad Ref!", []string{"--kind", "comment"}},
		{"annotation edit", "body", " ", []string{"0199a1b2"}},
		{"annotation edit", "kind", "note", []string{"0199a1b2"}},
		{"annotation edit", "source", "DB 2", []string{"0199a1b2"}},
		{"annotation edit", "url", " ", []string{"0199a1b2"}},
		{"annotation edit", "attach", "-", []string{"0199a1b2"}},
		{"annotation delete", "ids", "Bad Ref!", nil},
		{"attachment delete", "ids", "Bad Ref!", nil},
		{"overlay create", "name", "Checkout Incident", []string{"-t", "mongo-failover"}},
		{"source edit", "name", "Mongo DB", []string{"mongo"}},
	}
}

func badValueEntries() []TableEntry {
	entries := make([]TableEntry, 0, len(badValues()))
	for _, bad := range badValues() {
		entries = append(entries, Entry(fmt.Sprintf("%s %s %q", bad.path, bad.field, bad.value), bad))
	}

	return entries
}

// commandLine returns the arguments that give bad's field its value, as its positional argument
// or its flag, after bad's other arguments.
func commandLine(def clikit.Definition, bad badValue) []string {
	GinkgoHelper()
	args := append(strings.Fields(bad.path), bad.otherArgs...)

	return append(args, fieldArg(fieldNamed(def, bad.field), bad.value))
}

// fieldArg returns the argument that gives f value: value itself for the positional argument, or
// f's flag.
func fieldArg(f clikit.Field, value string) string {
	if f.Arg {
		return value
	}

	return "--" + f.Name + "=" + value
}

// flagError runs args, with a store that fails the spec when opened, and returns the *Error it
// fails with.
func flagError(ctx context.Context, args []string) *clikit.Error {
	GinkgoHelper()
	return executeError(ctx, storelessApp(args), args...)
}

// executeError runs args through app's root and returns the *Error it fails with.
func executeError(ctx context.Context, app clikit.App, args ...string) *clikit.Error {
	GinkgoHelper()
	root := NewRoot(ctx, app)
	root.SetArgs(args)

	_, err := root.ExecuteContextC(ctx)
	failure, ok := errors.AsType[*clikit.Error](err)
	Expect(ok).To(BeTrue(), "coroner %s returned %v", strings.Join(args, " "), err)

	return failure
}

// formError returns the error def's form field called name shows once it holds value and loses
// focus, with times read as the flag side reads them.
func formError(def clikit.Definition, name, value string) error {
	GinkgoHelper()
	formField := namedField(def.FormFields(formCheckEnv(), clikit.FieldValues{}, clikit.FormChoices{}), name)
	typeInto(formField, value)
	formField.Blur()

	return formField.Error()
}

// formCheckEnv is what form fields check time input against, as the flag side reads it.
func formCheckEnv() clikit.CheckEnv {
	GinkgoHelper()
	z, err := clikit.TimeZone(nil, "local", testNow().Location())
	Expect(err).NotTo(HaveOccurred())

	return clikit.CheckEnv{Now: testNow(), Zone: z}
}
