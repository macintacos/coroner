package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAttachmentExportCommand(ctx context.Context, app clikit.App) *cobra.Command {
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

func exportAttachment(ctx context.Context, cmd *cobra.Command, app clikit.App, ref string) error {
	prefix, err := parseAttachmentRef(ref)
	if err != nil {
		return err
	}
	inv, err := clikit.ReadInvocation(cmd, app)
	if err != nil {
		return err
	}
	path, err := clikit.OutputPath(cmd.Flags(), inv.Out.Format)
	if err != nil {
		return err
	}
	writeFile := NewFileWriter(app.OpenFile, app.RemoveFile)
	if force, _ := cmd.Flags().GetBool("force"); force {
		writeFile = app.WriteFile
	}
	if path == "-" && app.OutIsTTY {
		return &clikit.Error{
			Kind: clikit.KindUsage, Message: "attachment export writes the file's bytes, and stdout is a terminal",
			Hint: "Pass -o <file>, or redirect stdout to a file.",
		}
	}
	var a timeline.Attachment
	err = clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		a, err = findAttachment(ctx, s, prefix)
		return err
	})
	if err != nil {
		return err
	}
	clikit.LogRead(app.Logger(), clikit.CountOf("attachments", 1), slog.String("attachment", a.ID))
	content, err := app.ReadFile(store.BlobPath(inv.Cfg.DBPath, a.SHA256))
	if err != nil {
		return fmt.Errorf("read the blob of attachment %s: %w", a.ID, err)
	}

	err = writeAttachment(inv.Out, path, content, writeFile)
	if err != nil {
		return err
	}
	clikit.LogFileWritten(app.Logger(), path, len(content), slog.String("attachment", a.ID), slog.String("sha256", a.SHA256))

	return nil
}

// NewFileWriter returns a function that creates name with open and writes data to it, failing
// with an error that wraps fs.ErrExist when name exists, and removing name with remove when
// writing fails.
func NewFileWriter(
	open func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error), remove func(name string) error,
) func(name string, data []byte, perm fs.FileMode) error {
	return func(name string, data []byte, perm fs.FileMode) error {
		f, err := open(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		err = errors.Join(err, f.Close())
		if err != nil {
			return errors.Join(err, remove(name))
		}

		return nil
	}
}

// writeAttachment writes content to path with writeFile, or to o when path is -, reporting a
// file writeFile refuses to overwrite as file_exists.
func writeAttachment(o clikit.Output, path string, content []byte, writeFile func(string, []byte, fs.FileMode) error) error {
	_, err := o.WriteTo(path, writeFile, func(o clikit.Output) error {
		_, err := o.Out.Write(content)
		return err
	})
	if errors.Is(err, fs.ErrExist) {
		return &clikit.Error{
			Kind: clikit.KindFileExists, Message: path + " already exists", Hint: "Pass --force to overwrite it, or -o another file.", Err: err,
		}
	}

	return err
}
