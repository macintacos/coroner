package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAttachmentExportCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <id> [-o <file>]",
		Short: "Write an attachment's file back out",
		Long: "Write the file an attachment holds, byte for byte, to the file -o names, or to stdout with\n" +
			"-o -, the default, which refuses a terminal. -o never overwrites a file unless --force is\n" +
			"given. <id> is a full id or a unique id prefix, in any timeline.",
		Args: cobra.ExactArgs(1),
		Example: "  $ coroner attachment export 0199a1b2-7c1e -o grafana-p99.png\n" +
			"  $ coroner attachment export 0199a1b2-7c1e > grafana-p99.png",
		RunE: func(cmd *cobra.Command, args []string) error {
			return exportAttachment(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().StringP("output", "o", "-", "Write the attachment to this `file`, or to stdout when it is -")
	cmd.Flags().Bool("force", false, "Overwrite the file -o names when it exists")

	return cmd
}

func exportAttachment(ctx context.Context, cmd *cobra.Command, app App, ref string) error {
	prefix, err := parseAttachmentRef(ref)
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	path, err := outputPath(cmd.Flags(), o.format)
	if err != nil {
		return err
	}
	if path == "-" && app.OutIsTTY {
		return &Error{
			Kind: KindUsage, Message: "attachment export writes the file's bytes, and stdout is a terminal",
			Hint: "Pass -o <file>, or redirect stdout to a file.",
		}
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}

	var a timeline.Attachment
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		a, err = findAttachment(ctx, s, prefix)
		return err
	})
	if err != nil {
		return err
	}
	// Read and written only once the store is closed: withStorePath would report a permission
	// error from either as the database's.
	content, err := app.ReadFile(store.BlobPath(cfg.DBPath, a.SHA256))
	if err != nil {
		return fmt.Errorf("read the stored file of attachment %s: %w", a.ID, err)
	}

	return writeAttachment(o, path, content, attachmentWriter(cmd, app))
}

// attachmentWriter returns the file writer --force selects: one that overwrites, or one that
// refuses a file that exists.
func attachmentWriter(cmd *cobra.Command, app App) func(string, []byte, fs.FileMode) error {
	if force, _ := cmd.Flags().GetBool("force"); force {
		return app.WriteFile
	}

	return app.WriteNewFile
}

// writeAttachment writes content to path with writeFile, or to o when path is -, reporting a
// file writeFile refuses to overwrite as file_exists.
func writeAttachment(o output, path string, content []byte, writeFile func(string, []byte, fs.FileMode) error) error {
	err := o.writeTo(path, writeFile, func(o output) error {
		_, err := o.out.Write(content)
		return err
	})
	if errors.Is(err, fs.ErrExist) {
		return &Error{
			Kind: KindFileExists, Message: path + " already exists", Hint: "Pass --force to overwrite it, or -o another file.", Err: err,
		}
	}

	return err
}
