package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// writeDefinitions returns every write command's definition, by its path below coroner.
func writeDefinitions() map[string]definition {
	return map[string]definition{
		"timeline create": timelineCreateDefinition(),
		"timeline edit":   timelineEditDefinition(),
		"timeline delete": timelineDeleteDefinition(),
		"timeline use":    timelineUseDefinition(),
		"event add":       eventAddDefinition(),
		"event edit":      eventEditDefinition(),
		"event delete":    eventDeleteDefinition(),
		"event link":      eventLinkDefinition(),
		"event unlink":    eventUnlinkDefinition(),

		"annotation add":    annotationAddDefinition(),
		"annotation edit":   annotationEditDefinition(),
		"annotation delete": annotationDeleteDefinition(),

		"attachment delete": attachmentDeleteDefinition(),

		"overlay create": overlayCreateDefinition(),
	}
}

// formExemptFlags are the output and process globals a form never asks for, and --interactive,
// which opens the form. tz changes what is written only where a command reads typed times, and
// those definitions declare it.
func formExemptFlags() []string {
	return []string{"db", "config", "format", "json", "quiet", "no-color", "no-input", "debug", "help", "tz", "interactive"}
}

var _ = Describe("write command definitions", func() {
	It("give every flag a form field with its help and default, and every form field a flag", func(ctx SpecContext) {
		root := NewRoot(ctx, App{})

		var problems []string
		for path, def := range writeDefinitions() {
			problems = append(problems, parityProblems(root, path, def)...)
		}

		Expect(problems).To(BeEmpty())
	})

	It("accept every optional form field's zero value, which a form reads as not given, and reject a required one's", func() {
		var problems []string
		for path, def := range writeDefinitions() {
			for i, formField := range def.formFields(formCheckEnv(), fieldValues{}, formChoices{}) {
				formField.Blur()
				err := formField.Error()
				switch required := def.fields[i].required; {
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
			for _, missing := range def.fields {
				if !missing.required {
					continue
				}
				args := strings.Fields(path)
				for _, f := range def.fields {
					if f.required && f.name != missing.name {
						Expect(given).To(HaveKey(f.name), "no value to give coroner %s's required field", path)
						args = append(args, fieldArg(f, given[f.name]))
					}
				}

				Expect(flagError(ctx, args)).To(HaveField("Kind", KindUsage), "coroner %s", strings.Join(args, " "))
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
			for _, f := range def.fields {
				// -t's and --add-ref's checks reject only a blank value, which a form reads as not given.
				exempt := (f.name == "timeline" && !f.arg) || f.name == "add-ref"
				if f.check != nil && !exempt && !covered[path+" "+f.name] {
					missing = append(missing, path+" "+f.name)
				}
			}
		}

		Expect(missing).To(BeEmpty())
	})
})

var _ = Describe("definition", func() {
	def := definition{fields: []field{
		{name: "target", arg: true},
		{name: "title"},
		{name: "ref", typ: stringsField},
		{name: "yes", typ: boolField},
		{name: "untouched"},
	}}

	It("reads the arguments and only the flags given, keeping a repeatable flag's lone empty value", func() {
		flags := pflag.NewFlagSet("throwaway", pflag.ContinueOnError)
		def.addFlags(flags)
		Expect(flags.Parse([]string{"--ref", "", "--yes=false", "--title", "Primary election storm"})).To(Succeed())

		Expect(def.read(flags, []string{"12", "14"})).To(Equal(fieldValues{
			"target": {"12", "14"}, "title": {"Primary election storm"}, "ref": {""}, "yes": {"false"},
		}))
	})

	It("tells a boolean given false from one not given", func() {
		in := fieldValues{"yes": {"false"}}

		value, given := in.boolean("yes")
		Expect(value).To(BeFalse())
		Expect(given).To(BeTrue())
		_, given = in.boolean("untouched")
		Expect(given).To(BeFalse())
	})

	It("checks every value of a repeatable field, by flag and by form line", func() {
		rejectBad := withoutEnv(func(tag string) error {
			if tag == "Bad Tag" {
				return errors.New("bad tag")
			}
			return nil
		})
		tags := definition{fields: []field{{name: "tag", typ: stringsField, check: rejectBad}}}

		Expect(tags.check(fieldValues{"tag": {"election", "Bad Tag"}}, checkEnv{})).To(MatchError("bad tag"))
		Expect(formError(tags, "tag", "election\nBad Tag")).To(MatchError("bad tag"))
		Expect(formError(tags, "tag", "")).To(Succeed())
	})
})

var _ = Describe("write run functions", func() {
	DescribeTable("return usage, not a panic, when given no value for the positional argument their command requires",
		func(ctx SpecContext, run func(context.Context, App, config.Config, fieldValues) error, in fieldValues) {
			app := App{
				In: strings.NewReader(""), Now: testNow,
				NewID: func() (uuid.UUID, error) { return uuid.UUID{}, nil },
				OpenStore: func(context.Context, string) (*store.Store, error) {
					Fail("opened the store before rejecting its input")
					return nil, errors.New("unreachable")
				},
			}

			err := run(ctx, app, config.Config{DBPath: "/nonexistent/coroner.db", DefaultTimezone: "local"}, in)

			failure, ok := errors.AsType[*Error](err)
			Expect(ok).To(BeTrue(), "returned %v", err)
			Expect(failure.Kind).To(Equal(KindUsage))
		},
		Entry("timeline create", errorOf(createTimeline), fieldValues{}),
		Entry("timeline edit", errorOf(editTimeline), fieldValues{"title": {"Primary election storm"}}),
		Entry("timeline delete", errorOf(deleteTimeline), fieldValues{"yes": {"true"}}),
		Entry("timeline use", errorOf(useTimeline), fieldValues{}),
		Entry("event edit", errorOf(editEvent), fieldValues{"author": {"sre"}}),
		Entry("event delete", errorOf(deleteEvents), fieldValues{}),
		Entry("event link", errorOf(linkEvents), fieldValues{"kind": {"led_to"}}),
		Entry("event unlink", errorOf(unlinkEvents), fieldValues{"kind": {"led_to"}}),
		Entry("annotation edit", errorOf(editAnnotation), fieldValues{"kind": {"comment"}}),
		Entry("annotation delete", errorOf(deleteAnnotations), fieldValues{}),
		Entry("attachment delete", errorOf(deleteAttachments), fieldValues{}),
		Entry("overlay create", errorOf(createOverlay), fieldValues{"timeline": {"mongo-failover"}}),
	)
})

// errorOf returns run with its result dropped.
func errorOf[R any](
	run func(context.Context, App, config.Config, fieldValues) (R, error),
) func(context.Context, App, config.Config, fieldValues) error {
	return func(ctx context.Context, app App, cfg config.Config, in fieldValues) error {
		_, err := run(ctx, app, cfg, in)
		return err
	}
}

// parityProblems names each flag of the command at path with no matching form field, each form
// field with no flag, each field declared twice, and how a matching pair differs.
func parityProblems(root *cobra.Command, path string, def definition) []string {
	cmd, _, err := root.Find(strings.Fields(path))
	if err != nil || cmd.CommandPath() != "coroner "+path {
		return []string{"coroner " + path + ": no such command"}
	}

	problems := duplicateFieldProblems(path, def)
	form := map[string]huh.Field{}
	declared := map[string]field{}
	for i, formField := range def.formFields(checkEnv{}, fieldValues{}, formChoices{}) {
		// A positional argument's form field never stands for a flag.
		if !def.fields[i].arg {
			form[formField.GetKey()] = formField
			declared[def.fields[i].name] = def.fields[i]
		}
	}
	flags := pflag.NewFlagSet(path, pflag.ContinueOnError)
	flags.AddFlagSet(cmd.LocalFlags())
	flags.AddFlagSet(cmd.InheritedFlags())

	flags.VisitAll(func(flag *pflag.Flag) {
		formField, ok := form[flag.Name]
		switch {
		case !ok && !slices.Contains(formExemptFlags(), flag.Name):
			problems = append(problems, fmt.Sprintf("coroner %s: flag --%s has no form field", path, flag.Name))
		case ok:
			problems = append(problems, fieldProblems(path, flag, formField)...)
			problems = append(problems, flagShapeProblems(path, flag, declared[flag.Name])...)
		}
	})
	for _, f := range def.fields {
		if !f.arg && flags.Lookup(f.name) == nil {
			problems = append(problems, fmt.Sprintf("coroner %s: form field %s has no flag", path, f.name))
		}
	}

	return problems
}

// duplicateFieldProblems names each field def declares more than once.
func duplicateFieldProblems(path string, def definition) []string {
	var problems []string
	seen := map[string]bool{}
	for _, f := range def.fields {
		if seen[f.name] {
			problems = append(problems, fmt.Sprintf("coroner %s: field %s is declared twice", path, f.name))
		}
		seen[f.name] = true
	}

	return problems
}

// flagShapeProblems names how flag's type or shorthand differs from the field f declares.
func flagShapeProblems(path string, flag *pflag.Flag, f field) []string {
	var problems []string
	flagType := map[fieldType]string{stringField: "string", boolField: "bool", stringsField: "stringArray"}[f.typ]
	if flag.Value.Type() != flagType {
		problems = append(problems, fmt.Sprintf("coroner %s: field %s is a %s, flag --%s a %s",
			path, f.name, flagType, flag.Name, flag.Value.Type()))
	}
	if flag.Shorthand != f.shorthand {
		problems = append(problems, fmt.Sprintf("coroner %s: field %s has shorthand %q, flag --%s %q",
			path, f.name, f.shorthand, flag.Name, flag.Shorthand))
	}

	return problems
}

// fieldProblems names how field's help text or default differs from flag's.
func fieldProblems(path string, flag *pflag.Flag, formField huh.Field) []string {
	var problems []string
	_, usage := pflag.UnquoteUsage(flag)
	view, _ := formField.WithWidth(400).(interface{ View() string })
	if view == nil || !strings.Contains(ansi.Strip(view.View()), usage) {
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

// testNow is the clock both the flag and the form side of the validation table read.
func testNow() time.Time {
	return time.Date(2026, 9, 12, 21, 30, 0, 0, time.UTC)
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
func commandLine(def definition, bad badValue) []string {
	args := append(strings.Fields(bad.path), bad.otherArgs...)
	i := slices.IndexFunc(def.fields, func(f field) bool { return f.name == bad.field })
	Expect(i).NotTo(Equal(-1), "%s has no field %s", bad.path, bad.field)

	return append(args, fieldArg(def.fields[i], bad.value))
}

// fieldArg returns the argument that gives f value: value itself for the positional argument, or
// f's flag.
func fieldArg(f field, value string) string {
	if f.arg {
		return value
	}

	return "--" + f.name + "=" + value
}

// flagError runs args, with a store that fails the spec when opened, and returns the *Error it
// fails with.
func flagError(ctx context.Context, args []string) *Error {
	app := App{
		Args: args, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard,
		LookupEnv: func(key string) (string, bool) {
			value, ok := map[string]string{"HOME": "/nonexistent", "CORONER_DB": "/nonexistent/coroner.db"}[key]
			return value, ok
		},
		ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		Now:      testNow,
		NewID:    func() (uuid.UUID, error) { return uuid.UUID{}, nil },
		OpenStore: func(context.Context, string) (*store.Store, error) {
			Fail("coroner " + strings.Join(args, " ") + " opened the store before rejecting its input")
			return nil, errors.New("unreachable")
		},
	}
	root := NewRoot(ctx, app)
	root.SetArgs(args)

	_, err := root.ExecuteContextC(ctx)
	failure, ok := errors.AsType[*Error](err)
	Expect(ok).To(BeTrue(), "coroner %s returned %v", strings.Join(args, " "), err)

	return failure
}

// formError returns the error def's form field called name shows once it holds value and loses
// focus, with times read as the flag side reads them.
func formError(def definition, name, value string) error {
	for _, formField := range def.formFields(formCheckEnv(), fieldValues{}, formChoices{}) {
		if formField.GetKey() != name {
			continue
		}
		typeInto(formField, value)
		formField.Blur()

		return formField.Error()
	}

	Fail("no form field " + name)
	return nil
}

// formCheckEnv is what form fields check time input against, as the flag side reads it.
func formCheckEnv() checkEnv {
	z, err := timeZone(nil, "local", testNow().Location())
	Expect(err).NotTo(HaveOccurred())

	return checkEnv{now: testNow(), zone: z}
}
