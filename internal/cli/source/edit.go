package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
)

func newSourceEditCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := EditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <name> --name <new-name>",
		Short: "Rename a source",
		Long: "Rename a source everywhere it is used. Events and annotations refer to a source by id,\n" +
			"so they all follow the new name. A name another source has is refused: merging sources\n" +
			"is not supported yet.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		Annotations:       map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner source edit mongo --name mongodb --dry-run\n" +
			"  $ coroner source edit mongo --name db-2/mongod",
		RunE: clikit.WriteRunE(ctx, app, def, Edit),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// EditDefinition declares `source edit`'s flags, argument and their checks.
func EditDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{Name: "source", Arg: true, Usage: "Source to rename, as its `name`"},
		{Name: "name", Usage: "Rename it to `new-name`", Check: clikit.WithoutEnv(clikit.CheckSource), Changes: true},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show how many events and annotations the rename touches, and change nothing"},
	}}
}

// Edited is the source as edit renamed it or, with --dry-run, would rename it, and the name it had.
type Edited struct {
	source  store.SourceUsage
	oldName string
	dryRun  bool
	fields  []string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (e Edited) LogChanges(ctx context.Context, logger *slog.Logger) {
	clikit.WithDryRun(logger, e.dryRun).InfoContext(ctx, "source edited", "source_id", e.source.ID, "fields", e.fields)
}

// Edit runs `source edit` from in's values and returns what it changed, or would change under --dry-run.
func Edit(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Edited, error) {
	def := EditDefinition()
	err := def.Check(in, clikit.CheckEnv{})
	if err != nil {
		return Edited{}, err
	}
	oldName := in.Value("source")
	if oldName == nil {
		return Edited{}, clikit.MissingValue("source edit", "the source's name")
	}
	err = def.RequireChange(in)
	if err != nil {
		return Edited{}, err
	}
	newName := *in.Value("name")
	dryRun, _ := in.Boolean("dry-run")

	edited, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Edited, error) {
			// ponytail: reads every source's usage to find one; query by name if sources grow large.
			usages, err := tx.SourceUsages(ctx)
			if err != nil {
				return Edited{}, err
			}
			source, err := findSource(usages, *oldName)
			if err != nil {
				return Edited{}, err
			}
			edited := Edited{source: source, oldName: *oldName, dryRun: dryRun}
			edited.source.Name = newName
			edited.fields = clikit.ChangedFields(export.NewSource(source.Source), export.NewSource(edited.source.Source))

			err = tx.RenameSource(ctx, source.ID, newName)
			if errors.Is(err, store.ErrConflict) {
				return edited, sourceExistsError(newName, err)
			}

			return edited, err
		})

	return edited, err
}

// Render prints the result in o's format.
func (e Edited) Render(o clikit.Output) error {
	if e.dryRun {
		return previewSourceEdit(o, e.source, e.oldName)
	}
	err := printSource(o, e.source)
	if err != nil {
		return err
	}

	return o.Notify("Renamed source `%s` to `%s`.", e.oldName, e.source.Name)
}

// findSource returns the usage of the source called name, or source_not_found.
func findSource(usages []store.SourceUsage, name string) (store.SourceUsage, error) {
	i := slices.IndexFunc(usages, func(u store.SourceUsage) bool { return u.Name == name })
	if i < 0 {
		return store.SourceUsage{}, &clikit.Error{
			Kind:    clikit.KindSourceNotFound,
			Message: fmt.Sprintf("no source is named %q", name),
			Hint:    "Run 'coroner source list' to see the sources.",
		}
	}

	return usages[i], nil
}

// previewSourceEdit prints what renaming the source called oldName to renamed would touch.
func previewSourceEdit(o clikit.Output, renamed store.SourceUsage, oldName string) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(newListedSourceJSON(renamed))
	}

	_, err := fmt.Fprintf(o.Out, "Would rename source `%s` to `%s`: events: %d, annotations: %d\n",
		oldName, renamed.Name, renamed.EventCount, renamed.AnnotationCount)
	return err
}

// printSource prints u when o asks for JSON; text output leaves it to the command's message.
func printSource(o clikit.Output, u store.SourceUsage) error {
	if o.Format != clikit.FormatJSON {
		return nil
	}

	return o.JSON(newListedSourceJSON(u))
}

func sourceExistsError(name string, err error) *clikit.Error {
	return &clikit.Error{
		Kind:    clikit.KindSourceExists,
		Message: fmt.Sprintf("source %q already exists", name),
		Hint:    "Merging sources is not supported yet; pick another --name.",
		Err:     err,
	}
}
