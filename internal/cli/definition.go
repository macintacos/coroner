package cli

import (
	"context"
	"strconv"
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
	// usage is the flag's usage, with its `placeholder`.
	usage string
	typ   fieldType
	// arg marks the positional argument, which cobra's Args counts and no flag registers.
	arg bool
	// inherited marks a flag a parent command or root registers, such as -t on event or --tz.
	inherited bool
	// check rejects a value no command could take; nil accepts any.
	check func(value string, env checkEnv) error
}

// checkEnv is what a check reads time input against.
type checkEnv struct {
	now  time.Time
	zone zone
}

// definition is a write command's inputs, from which both its flags and its form fields are
// built.
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

// withoutEnv makes check, which reads no time input, a field check.
func withoutEnv(check func(string) error) func(string, checkEnv) error {
	return func(value string, _ checkEnv) error {
		return check(value)
	}
}

// writeRunE returns the RunE of a write command that prints only what run returns: it reads
// def's fields from the command line, runs run against them and renders the result.
func writeRunE[R interface{ render(o output) error }](
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
