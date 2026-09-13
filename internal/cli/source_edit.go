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
		Use:   "edit <name> --name <new>",
		Short: "Rename a source",
		Long: "Rename a source everywhere it is used. Events and annotations refer to a source by id,\n" +
			"so they all follow the new name. A name another source has is refused: merging sources\n" +
			"is not supported.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner source edit mongo --name mongodb --dry-run\n" +
			"  $ coroner source edit mongo --name db-2/mongod",
		RunE: func(cmd *cobra.Command, args []string) error {
			return editSource(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().String("name", "", "New `name`")
	cmd.Flags().Bool("dry-run", false, "Show how many events and annotations the rename touches, and change nothing")

	return cmd
}

func editSource(ctx context.Context, cmd *cobra.Command, app App, name string) error {
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
		// ponytail: every source's usage is read to find one; a by-name query if a database
		// ever holds enough sources to notice. The read and the rename are separate
		// transactions, so a concurrent edit of this source is overwritten.
		usages, err := s.SourceUsages(ctx)
		if err != nil {
			return err
		}
		source, err := findSource(usages, name)
		if err != nil {
			return err
		}
		renamed := source
		renamed.Name = newName

		if dryRun {
			return previewSourceEdit(o, usages, source, renamed)
		}

		err = s.RenameSource(ctx, source.ID, newName)
		if errors.Is(err, store.ErrConflict) {
			return sourceExistsError(newName, err)
		}
		if err != nil {
			return err
		}
		if o.format == formatJSON {
			err = o.json(newListedSourceJSON(renamed))
			if err != nil {
				return err
			}
		}

		return o.notify("Renamed source `%s` to `%s`.", name, newName)
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

// previewSourceEdit prints what renaming source to renamed would touch, failing as the rename
// would when another source has the new name.
func previewSourceEdit(o output, usages []store.SourceUsage, source, renamed store.SourceUsage) error {
	holder, err := findSource(usages, renamed.Name)
	if err == nil && holder.ID != source.ID {
		return sourceExistsError(renamed.Name, nil)
	}
	if o.format == formatJSON {
		return o.json(newListedSourceJSON(renamed))
	}

	_, err = fmt.Fprintf(o.out, "Would rename source `%s` to `%s`: events: %d, annotations: %d\n",
		source.Name, renamed.Name, source.EventCount, source.AnnotationCount)
	return err
}

func sourceExistsError(name string, err error) *Error {
	return &Error{
		Kind:    KindSourceExists,
		Message: fmt.Sprintf("source %q already exists", name),
		Hint:    "Merging sources is not supported yet; pick another --name.",
		Err:     err,
	}
}
