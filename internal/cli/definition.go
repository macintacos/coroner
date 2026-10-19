package cli

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
)

type fieldType int

const (
	stringField fieldType = iota
	boolField
	// stringsField repeats: each use of its flag, or each line of its form field, is one value.
	stringsField
)

// field is one input a write command takes: a flag, or the command's positional argument.
type field struct {
	name, shorthand string
	// usage is the flag's usage, or a positional field's form description, with its `placeholder`.
	usage string
	typ   fieldType
	// arg marks the positional argument, which cobra's Args counts and no flag registers: at most
	// one per definition; it receives every argument.
	arg bool
	// inherited marks a flag a parent command or root registers, such as -t on event or --tz.
	inherited bool
	// check rejects a value no command could take; nil accepts any.
	check func(value string, env checkEnv) error
	// required marks an input the command cannot run without. Only a form reads it; each run
	// function reports its own missing value.
	required bool
	// changes marks a field that changes the record; an edit needs at least one.
	changes bool
	// perEntry marks a field another form entry opens without.
	perEntry bool
	// preview renders, under the form field, what value resolves to; nil shows only a failing check's hint.
	preview func(value string, env checkEnv) string
	// list makes a repeatable field's form field take its values one at a time instead of one per line.
	list bool
	// suggest returns the valid value a form field offers in place of a rejected one, or "" for none.
	suggest func(value string) string
}

// checkEnv is what a check reads time input against.
type checkEnv struct {
	now  time.Time
	zone zone
}

// definition is a write command's inputs, from which both its flags and its form fields are
// built. A run function calls check before it uses any value; reading flags does not.
type definition struct {
	fields []field
}

// addFlags registers each field the command itself declares as a flag.
func (d definition) addFlags(flags *pflag.FlagSet) {
	for _, f := range d.fields {
		switch {
		case f.arg || f.inherited:
		case f.typ == boolField:
			flags.BoolP(f.name, f.shorthand, false, f.usage)
		case f.typ == stringsField:
			flags.StringArrayP(f.name, f.shorthand, nil, f.usage)
		default:
			flags.StringP(f.name, f.shorthand, "", f.usage)
		}
	}
}

// read returns the values the command line gives d's fields: args for the positional argument,
// and each flag that was given.
func (d definition) read(flags *pflag.FlagSet, args []string) fieldValues {
	values := fieldValues{}
	for _, f := range d.fields {
		switch {
		case f.arg && len(args) > 0:
			values[f.name] = args
		case f.arg || !flags.Changed(f.name):
		case f.typ == stringsField:
			values[f.name] = stringArray(flags, f.name)
		default:
			values[f.name] = []string{flags.Lookup(f.name).Value.String()}
		}
	}

	return values
}

// check returns the first error a field's check finds in values, in field order.
func (d definition) check(values fieldValues, env checkEnv) error {
	for _, f := range d.fields {
		if f.check == nil {
			continue
		}
		for _, value := range values[f.name] {
			err := f.check(value, env)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// changed reports whether in gives a field that changes the record.
func (d definition) changed(in fieldValues) bool {
	return slices.ContainsFunc(d.fields, func(f field) bool { return f.changes && in.given(f.name) })
}

// requireChange returns usage naming d's changing flags, in field order, when in gives none of them.
func (d definition) requireChange(in fieldValues) error {
	if d.changed(in) {
		return nil
	}
	var flags []string
	for _, f := range d.fields {
		if f.changes {
			flags = append(flags, "--"+f.name)
		}
	}
	hint := strings.Join(flags, ", ")
	if i := strings.LastIndex(hint, ", "); i >= 0 {
		hint = hint[:i] + " or " + hint[i+2:]
	}

	return &Error{Kind: KindUsage, Message: "nothing to edit", Hint: "Pass " + hint + "."}
}

// missingValue returns usage for a run function given no value for a field its command requires.
func missingValue(command, what string) *Error {
	return &Error{Kind: KindUsage, Message: command + " needs " + what, Hint: "Run 'coroner " + command + " --help' for what it takes."}
}

// inherited returns f registered by a parent command rather than the command itself.
func inherited(f field) field {
	f.inherited = true
	return f
}

// changing returns f marked as a field that changes the record.
func changing(f field) field {
	f.changes = true
	return f
}

// perEntry returns f marked as a field another form entry opens without.
func perEntry(f field) field {
	f.perEntry = true
	return f
}

// withoutEnv makes check, which reads no time input, a field check.
func withoutEnv(check func(string) error) func(string, checkEnv) error {
	return func(value string, _ checkEnv) error {
		return check(value)
	}
}

type renderer interface{ render(o output) error }

// writeRunE returns the RunE of a write command that prints only what run returns: it reads
// def's fields from the command line, runs run against them and renders the result.
func writeRunE[R renderer](
	ctx context.Context, app App, def definition, run func(context.Context, App, config.Config, fieldValues) (R, error),
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		o, err := newOutput(cmd, app)
		if err != nil {
			return err
		}
		cfg, err := resolveConfig(cmd, app)
		if err != nil {
			return err
		}
		result, err := run(ctx, app, cfg, def.read(cmd.Flags(), args))
		if err != nil {
			return err
		}

		return result.render(o)
	}
}

// fieldValues are the values given for a definition's fields, by field name. A field that was
// not given has no entry.
type fieldValues map[string][]string

// value returns the field's value, or nil when it was not given.
func (v fieldValues) value(name string) *string {
	values := v[name]
	if len(values) == 0 {
		return nil
	}

	return new(values[0])
}

func (v fieldValues) values(name string) []string {
	return v[name]
}

// boolean returns the field's value and whether it was given.
func (v fieldValues) boolean(name string) (bool, bool) {
	value := v.value(name)
	if value == nil {
		return false, false
	}
	b, _ := strconv.ParseBool(*value)

	return b, true
}

func (v fieldValues) given(name string) bool {
	_, ok := v[name]
	return ok
}
