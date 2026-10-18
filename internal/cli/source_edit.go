package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newSourceEditCommand(ctx context.Context, app App) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return editSource(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().String("name", "", "Rename it to `new-name`")
	cmd.Flags().Bool("dry-run", false, "Show how many events and annotations the rename touches, and change nothing")

	return cmd
}

func editSource(ctx context.Context, cmd *cobra.Command, app App, oldName string) error {
	newName, err := readSourceName(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		// ponytail: reads every source's usage to find one; query by name if sources grow large.
		// ponytail: the read and rename are separate transactions, so a concurrent edit is
		// overwritten; rename inside one store write if that matters.
		usages, err := s.SourceUsages(ctx)
		if err != nil {
			return err
		}
		source, err := findSource(usages, oldName)
		if err != nil {
			return err
		}
		renamed := source
		renamed.Name = newName

		if dryRun {
			return previewSourceEdit(o, usages, renamed, oldName)
		}

		err = s.RenameSource(ctx, source.ID, newName)
		if errors.Is(err, store.ErrConflict) {
			return sourceExistsError(newName, err)
		}
		if err != nil {
			return err
		}
		err = printSource(o, renamed)
		if err != nil {
			return err
		}

		return o.notify("Renamed source `%s` to `%s`.", oldName, newName)
	})
}

// readSourceName returns --name, rejecting a missing or invalid one before the store opens.
func readSourceName(cmd *cobra.Command) (string, error) {
	newName := changedString(cmd.Flags(), "name")
	if newName == nil {
		return "", &Error{Kind: KindUsage, Message: "nothing to edit", Hint: "Pass --name with the source's new name."}
	}

	return *newName, checkSlug(*newName, timeline.ValidateSourceName)
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

// previewSourceEdit prints what renaming the source called oldName to renamed would touch,
// failing as the rename would when another source has the new name.
func previewSourceEdit(o output, usages []store.SourceUsage, renamed store.SourceUsage, oldName string) error {
	taken := slices.ContainsFunc(usages, func(u store.SourceUsage) bool {
		return u.Name == renamed.Name && u.ID != renamed.ID
	})
	if taken {
		return sourceExistsError(renamed.Name, nil)
	}
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
