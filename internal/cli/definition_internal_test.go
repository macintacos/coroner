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

	"github.com/macintacos/coroner/internal/store"
)

// writeDefinitions returns every write command's definition, by its path below coroner.
func writeDefinitions() map[string]definition {
	return map[string]definition{
		"timeline create": timelineCreateDefinition(),
		"timeline edit":   timelineEditDefinition(),
		"timeline delete": timelineDeleteDefinition(),
		"event add":       eventAddDefinition(),
		"event edit":      eventEditDefinition(),
		"event delete":    eventDeleteDefinition(),
	}
}

// formExemptFlags are the output and process globals a form never asks for. tz changes what is
// written only where a command reads typed times, and those definitions declare it.
func formExemptFlags() []string {
	return []string{"db", "config", "format", "json", "quiet", "no-color", "debug", "help", "tz"}
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

	DescribeTable("reject a bad value the same way by flag and by form field",
		func(ctx SpecContext, path, name, value string, otherArgs ...string) {
			def, ok := writeDefinitions()[path]
			Expect(ok).To(BeTrue(), "no definition for %q", path)

			byFlag := flagError(ctx, commandLine(def, path, name, value, otherArgs))
			byForm := formError(def, name, value)

			Expect(byForm).To(SatisfyAll(
				HaveField("Kind", byFlag.Kind), HaveField("Message", byFlag.Message), HaveField("Hint", byFlag.Hint)))
		},
		Entry("timeline create <name>", "timeline create", "name", "Mongo Outage"),
		Entry("timeline edit --name", "timeline edit", "name", "Bad Name", "mongo-outage"),
		Entry("timeline edit --add-ref", "timeline edit", "add-ref", " ", "mongo-outage"),
		Entry("event add <text>", "event add", "text", " "),
		Entry("event add -t", "event add", "timeline", "", "Pager fires"),
		Entry("event add --tz", "event add", "tz", "Mars/Olympus", "Pager fires"),
		Entry("event add --at", "event add", "at", "2026-13-45", "Pager fires"),
		Entry("event add --ended", "event add", "ended", "~2026-09-11T03:14:00Z", "Pager fires"),
		Entry("event add --source", "event add", "source", "DB 2", "Pager fires"),
		Entry("event add --tag", "event add", "tag", "Bad Tag", "Pager fires"),
		Entry("event add --url", "event add", "url", " ", "Pager fires"),
		Entry("event add --author", "event add", "author", " ", "Pager fires"),
		Entry("event edit <event>", "event edit", "event", "Bad Ref!", "--dry-run"),
		Entry("event edit --text", "event edit", "text", " ", "12"),
		Entry("event edit --at", "event edit", "at", "2026-13-45", "12"),
		Entry("event edit --ended", "event edit", "ended", "~2026-09-11T03:14:00Z", "12"),
		Entry("event edit --source", "event edit", "source", " ", "12"),
		Entry("event edit --add-tag", "event edit", "add-tag", "Bad Tag", "12"),
		Entry("event edit --url", "event edit", "url", " ", "12"),
		Entry("event delete <event>...", "event delete", "events", "Bad Ref!"),
	)
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

// parityProblems names each flag of the command at path with no matching form field, and each
// form field with no flag.
func parityProblems(root *cobra.Command, path string, def definition) []string {
	cmd, _, err := root.Find(strings.Fields(path))
	if err != nil || cmd.CommandPath() != "coroner "+path {
		return []string{"coroner " + path + ": no such command"}
	}

	form := map[string]huh.Field{}
	for _, field := range def.formFields(checkEnv{}) {
		form[field.GetKey()] = field
	}
	flags := pflag.NewFlagSet(path, pflag.ContinueOnError)
	flags.AddFlagSet(cmd.LocalFlags())
	flags.AddFlagSet(cmd.InheritedFlags())

	var problems []string
	flags.VisitAll(func(flag *pflag.Flag) {
		field, ok := form[flag.Name]
		switch {
		case !ok && !slices.Contains(formExemptFlags(), flag.Name):
			problems = append(problems, fmt.Sprintf("coroner %s: flag --%s has no form field", path, flag.Name))
		case ok:
			problems = append(problems, fieldProblems(path, flag, field)...)
		}
	})
	for _, f := range def.fields {
		if !f.arg && flags.Lookup(f.name) == nil {
			problems = append(problems, fmt.Sprintf("coroner %s: form field %s has no flag", path, f.name))
		}
	}

	return problems
}

// fieldProblems names how field's help text or default differs from flag's.
func fieldProblems(path string, flag *pflag.Flag, field huh.Field) []string {
	var problems []string
	_, usage := pflag.UnquoteUsage(flag)
	view, _ := field.WithWidth(400).(interface{ View() string })
	if view == nil || !strings.Contains(ansi.Strip(view.View()), usage) {
		problems = append(problems, fmt.Sprintf("coroner %s: form field %s does not show %q", path, flag.Name, usage))
	}

	// A repeatable flag's default [] is a Text field's empty string.
	flagDefault := strings.TrimSuffix(strings.TrimPrefix(flag.DefValue, "["), "]")
	if formDefault := fmt.Sprint(field.GetValue()); formDefault != flagDefault {
		problems = append(problems, fmt.Sprintf("coroner %s: form field %s defaults to %q, flag --%s to %q",
			path, flag.Name, formDefault, flag.Name, flag.DefValue))
	}

	return problems
}

// testNow is the clock both the flag and the form side of the validation table read.
func testNow() time.Time {
	return time.Date(2026, 9, 12, 21, 30, 0, 0, time.UTC)
}

// commandLine returns the arguments that give the field called name value, as its positional
// argument or its flag, after otherArgs.
func commandLine(def definition, path, name, value string, otherArgs []string) []string {
	args := append(strings.Fields(path), otherArgs...)
	i := slices.IndexFunc(def.fields, func(f field) bool { return f.name == name })
	Expect(i).NotTo(Equal(-1), "%s has no field %s", path, name)
	if def.fields[i].arg {
		return append(args, value)
	}

	return append(args, "--"+name+"="+value)
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
	z, err := timeZone(nil, "local", testNow().Location())
	Expect(err).NotTo(HaveOccurred())

	for _, field := range def.formFields(checkEnv{now: testNow(), zone: z}) {
		if field.GetKey() != name {
			continue
		}
		switch f := field.(type) {
		case *huh.Input:
			f.Value(&value)
		case *huh.Text:
			f.Value(&value)
		default:
			Fail(fmt.Sprintf("form field %s takes no text", name))
		}
		field.Blur()

		return field.Error()
	}

	Fail("no form field " + name)
	return nil
}
