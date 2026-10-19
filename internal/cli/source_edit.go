package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

func newSourceEditCommand(ctx context.Context, app App) *cobra.Command {
	def := sourceEditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <name> --name <new-name>",
		Short: "Rename a source",
		Long: "Rename a source everywhere it is used. Events and annotations refer to a source by id,\n" +
			"so they all follow the new name. A name another source has is refused: merging sources\n" +
			"is not supported yet.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner source edit mongo --name mongodb --dry-run\n" +
			"  $ coroner source edit mongo --name db-2/mongod",
		RunE: writeRunE(ctx, app, def, editSource),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func sourceEditDefinition() definition {
	return definition{fields: []field{
		{name: "source", arg: true, usage: "Source to rename, as its `name`"},
		{name: "name", usage: "Rename it to `new-name`", check: withoutEnv(checkSource), changes: true},
		{name: "dry-run", typ: boolField, usage: "Show how many events and annotations the rename touches, and change nothing"},
	}}
}

// sourceEdited is the source as edit renamed it or, with --dry-run, would rename it, and the name it had.
type sourceEdited struct {
	source  store.SourceUsage
	oldName string
	dryRun  bool
}

func editSource(ctx context.Context, app App, cfg config.Config, in fieldValues) (sourceEdited, error) {
	def := sourceEditDefinition()
	err := def.check(in, checkEnv{})
	if err != nil {
		return sourceEdited{}, err
	}
	oldName := in.value("source")
	if oldName == nil {
		return sourceEdited{}, missingValue("source edit", "the source's name")
	}
	err = def.requireChange(in)
	if err != nil {
		return sourceEdited{}, err
	}
	newName := *in.value("name")
	dryRun, _ := in.boolean("dry-run")

	edited, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (sourceEdited, error) {
			// ponytail: reads every source's usage to find one; query by name if sources grow large.
			usages, err := tx.SourceUsages(ctx)
			if err != nil {
				return sourceEdited{}, err
			}
			source, err := findSource(usages, *oldName)
			if err != nil {
				return sourceEdited{}, err
			}
			edited := sourceEdited{source: source, oldName: *oldName, dryRun: dryRun}
			edited.source.Name = newName

			err = tx.RenameSource(ctx, source.ID, newName)
			if errors.Is(err, store.ErrConflict) {
				return edited, sourceExistsError(newName, err)
			}

			return edited, err
		})

	return edited, err
}

func (e sourceEdited) render(o output) error {
	if e.dryRun {
		return previewSourceEdit(o, e.source, e.oldName)
	}
	err := printSource(o, e.source)
	if err != nil {
		return err
	}

	return o.notify("Renamed source `%s` to `%s`.", e.oldName, e.source.Name)
}

// findSource returns the usage of the source called name, or source_not_found.
func findSource(usages []store.SourceUsage, name string) (store.SourceUsage, error) {
	i := slices.IndexFunc(usages, func(u store.SourceUsage) bool { return u.Name == name })
	if i < 0 {
		return store.SourceUsage{}, &Error{
			Kind:    KindSourceNotFound,
			Message: fmt.Sprintf("no source is named %q", name),
			Hint:    "Run 'coroner source list' to see the sources.",
		}
	}

	return usages[i], nil
}

// previewSourceEdit prints what renaming the source called oldName to renamed would touch.
func previewSourceEdit(o output, renamed store.SourceUsage, oldName string) error {
	if o.format == formatJSON {
		return o.json(newListedSourceJSON(renamed))
	}

	_, err := fmt.Fprintf(o.out, "Would rename source `%s` to `%s`: events: %d, annotations: %d\n",
		oldName, renamed.Name, renamed.EventCount, renamed.AnnotationCount)
	return err
}

// printSource prints u when o asks for JSON; text output leaves it to the command's message.
func printSource(o output, u store.SourceUsage) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newListedSourceJSON(u))
}

func sourceExistsError(name string, err error) *Error {
	return &Error{
		Kind:    KindSourceExists,
		Message: fmt.Sprintf("source %q already exists", name),
		Hint:    "Merging sources is not supported yet; pick another --name.",
		Err:     err,
	}
}
