package clikit

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
)

// FieldType is the kind of value a field takes.
type FieldType int

// The field types.
const (
	StringField FieldType = iota
	BoolField
	// StringsField repeats: each use of its flag, or each line of its form field, is one value.
	StringsField
)

// Field is one input a write command takes: a flag, or the command's positional argument.
type Field struct {
	Name, Shorthand string
	// Usage is the flag's usage, or a positional field's form description, with its `placeholder`.
	Usage string
	Typ   FieldType
	// Arg marks the positional argument, which cobra's Args counts and no flag registers: at most
	// one per definition; it receives every argument.
	Arg bool
	// Inherited marks a flag a parent command or root registers, such as -t on event or --tz.
	Inherited bool
	// Check rejects a value no command could take; nil accepts any.
	Check func(value string, env CheckEnv) error
	// Required marks an input the command cannot run without. Only a form reads it; each run
	// function reports its own missing value.
	Required bool
	// Changes marks a field that changes the record; an edit needs at least one.
	Changes bool
	// PerEntry marks a field another add form entry opens without; only an add form reads it.
	PerEntry bool
	// More marks a field a form asks only behind More options; only a form reads it.
	More bool
	// Title is what a form calls the field, empty meaning its name; only a form reads it.
	Title string
	// Preview renders, under the form field, what value resolves to; nil shows only a failing check's hint.
	Preview func(value string, env CheckEnv) string
	// List makes a repeatable field's form field take its values one at a time instead of one per line.
	List bool
	// Suggest returns the valid value a form field offers in place of a rejected one, or "" for none.
	Suggest func(value string) string
}

// CheckEnv is what a check reads time input against.
type CheckEnv struct {
	Now  time.Time
	Zone Zone
}

// Definition is a write command's inputs, from which both its flags and its form fields are
// built. A run function calls check before it uses any value; reading flags does not.
type Definition struct {
	Fields []Field
}

// FieldIndex returns the index in d.fields of the field called name, or -1.
func (d Definition) FieldIndex(name string) int {
	return slices.IndexFunc(d.Fields, func(f Field) bool { return f.Name == name })
}

// AddFlags registers each field the command itself declares as a flag.
func (d Definition) AddFlags(flags *pflag.FlagSet) {
	for _, f := range d.Fields {
		switch {
		case f.Arg || f.Inherited:
		case f.Typ == BoolField:
			flags.BoolP(f.Name, f.Shorthand, false, f.Usage)
		case f.Typ == StringsField:
			flags.StringArrayP(f.Name, f.Shorthand, nil, f.Usage)
		default:
			flags.StringP(f.Name, f.Shorthand, "", f.Usage)
		}
	}
}

// Read returns the values the command line gives d's fields: args for the positional argument,
// and each flag that was given.
func (d Definition) Read(flags *pflag.FlagSet, args []string) FieldValues {
	values := FieldValues{}
	for _, f := range d.Fields {
		switch {
		case f.Arg && len(args) > 0:
			values[f.Name] = args
		case f.Arg || !flags.Changed(f.Name):
		case f.Typ == StringsField:
			values[f.Name] = stringArray(flags, f.Name)
		default:
			values[f.Name] = []string{flags.Lookup(f.Name).Value.String()}
		}
	}

	return values
}

// Check returns the first error a field's check finds in values, in field order.
func (d Definition) Check(values FieldValues, env CheckEnv) error {
	for _, f := range d.Fields {
		if f.Check == nil {
			continue
		}
		for _, value := range values[f.Name] {
			err := f.Check(value, env)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Changed reports whether in gives a field that changes the record.
func (d Definition) Changed(in FieldValues) bool {
	return slices.ContainsFunc(d.Fields, func(f Field) bool { return f.Changes && in.Given(f.Name) })
}

// RequireChange returns usage naming d's changing flags, in field order, when in gives none of them.
func (d Definition) RequireChange(in FieldValues) error {
	if d.Changed(in) {
		return nil
	}
	var flags []string
	for _, f := range d.Fields {
		if f.Changes {
			flags = append(flags, "--"+f.Name)
		}
	}
	hint := strings.Join(flags, ", ")
	if i := strings.LastIndex(hint, ", "); i >= 0 {
		hint = hint[:i] + " or " + hint[i+2:]
	}

	return &Error{Kind: KindUsage, Message: "nothing to edit", Hint: "Pass " + hint + "."}
}

// MissingValue returns usage for a run function given no value for a field its command requires.
func MissingValue(command, what string) *Error {
	return &Error{Kind: KindUsage, Message: command + " needs " + what, Hint: "Run 'coroner " + command + " --help' for what it takes."}
}

// Inherited returns f registered by a parent command rather than the command itself.
func Inherited(f Field) Field {
	f.Inherited = true
	return f
}

// Changing returns f marked as a field that changes the record.
func Changing(f Field) Field {
	f.Changes = true
	return f
}

// PerEntry returns f marked as a field another form entry opens without.
func PerEntry(f Field) Field {
	f.PerEntry = true
	return f
}

// More returns f marked as a field a form asks only behind More options.
func More(f Field) Field {
	f.More = true
	return f
}

// Titled returns f with the title a form calls it.
func Titled(f Field, title string) Field {
	f.Title = title
	return f
}

// FormTitle returns what a form calls f.
func (f Field) FormTitle() string {
	return cmp.Or(f.Title, f.Name)
}

// WithoutEnv makes check, which reads no time input, a field check.
func WithoutEnv(check func(string) error) func(string, CheckEnv) error {
	return func(value string, _ CheckEnv) error {
		return check(value)
	}
}

type renderer interface{ Render(o Output) error }

// changeLogger is a write command's result. LogChanges logs, by id, each record the write
// changed; it is called once, after the change succeeds and before render.
type changeLogger interface {
	LogChanges(ctx context.Context, logger *slog.Logger)
}

// writeResult is a write command's result printed without a zone.
type writeResult interface {
	renderer
	changeLogger
}

// zonedWriteResult is a write command's result printed in a zone.
type zonedWriteResult interface {
	zonedRenderer
	changeLogger
}

// WriteRunE returns the RunE of a write command that prints only what run returns: it reads
// def's fields from the command line, runs run against them, logs and renders the result.
func WriteRunE[R writeResult](
	ctx context.Context, app App, def Definition, run func(context.Context, App, config.Config, FieldValues) (R, error),
) func(*cobra.Command, []string) error {
	return withInvocation(app, WriteRun(ctx, app, def, run))
}

// WriteRun is WriteRunE for a caller that has already read the invocation.
func WriteRun[R writeResult](
	ctx context.Context, app App, def Definition, run func(context.Context, App, config.Config, FieldValues) (R, error),
) InvokedRunE {
	return func(cmd *cobra.Command, inv Invocation, args []string) error {
		result, err := run(ctx, app, inv.Cfg, def.Read(cmd.Flags(), args))
		if err != nil {
			return err
		}
		result.LogChanges(ctx, app.Logger())

		return result.Render(inv.Out)
	}
}

// InvokedRunE is a RunE handed the invocation its caller read.
type InvokedRunE func(cmd *cobra.Command, inv Invocation, args []string) error

// withInvocation returns a RunE that reads the invocation once and hands it to run.
func withInvocation(app App, run InvokedRunE) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		inv, err := ReadInvocation(cmd, app)
		if err != nil {
			return err
		}

		return run(cmd, inv, args)
	}
}

type zonedRenderer interface{ Render(o Output, z Zone) error }

// WriteRunEInZone is WriteRunE for a result printed in the zone, resolved before run writes anything.
func WriteRunEInZone[R zonedWriteResult](
	ctx context.Context, app App, def Definition, run func(context.Context, App, config.Config, FieldValues) (R, error),
) func(*cobra.Command, []string) error {
	return withInvocation(app, WriteRunInZone(ctx, app, def, run))
}

// WriteRunInZone is WriteRunEInZone for a caller that has already read the invocation.
func WriteRunInZone[R zonedWriteResult](
	ctx context.Context, app App, def Definition, run func(context.Context, App, config.Config, FieldValues) (R, error),
) InvokedRunE {
	return func(cmd *cobra.Command, inv Invocation, args []string) error {
		z, err := inv.Zone()
		if err != nil {
			return err
		}
		result, err := run(ctx, app, inv.Cfg, def.Read(cmd.Flags(), args))
		if err != nil {
			return err
		}
		result.LogChanges(ctx, app.Logger())

		return result.Render(inv.Out, z)
	}
}

// FieldValues are the values given for a definition's fields, by field name. A field that was
// not given has no entry.
type FieldValues map[string][]string

// Value returns the field's value, or nil when it was not given.
func (v FieldValues) Value(name string) *string {
	values := v[name]
	if len(values) == 0 {
		return nil
	}

	return new(values[0])
}

// Values returns every value given for the field, or nil when it was not given.
func (v FieldValues) Values(name string) []string {
	return v[name]
}

// Boolean returns the field's value and whether it was given.
func (v FieldValues) Boolean(name string) (bool, bool) {
	value := v.Value(name)
	if value == nil {
		return false, false
	}
	b, _ := strconv.ParseBool(*value)

	return b, true
}

// Given reports whether the field was given.
func (v FieldValues) Given(name string) bool {
	_, ok := v[name]
	return ok
}
