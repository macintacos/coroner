// Command coroner is the coroner CLI. It builds a cli.App from the process and exits with
// cli.Run's code.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	// Embeds the zone database so --tz and default_timezone resolve on hosts without one.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/mattn/go-isatty"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/skills"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Restoring default signal handling once ctx is done lets a second Ctrl-C kill at once.
	context.AfterFunc(ctx, stop)

	version := "(devel)"
	if build, ok := debug.ReadBuildInfo(); ok {
		version = build.Main.Version
	}
	code := cli.Run(ctx, cli.App{
		Args:              os.Args[1:],
		In:                os.Stdin,
		Out:               os.Stdout,
		Err:               os.Stderr,
		InIsTTY:           isatty.IsTerminal(os.Stdin.Fd()),
		OutIsTTY:          isatty.IsTerminal(os.Stdout.Fd()),
		ErrIsTTY:          isatty.IsTerminal(os.Stderr.Fd()),
		Version:           version,
		Skill:             skills.Coroner,
		LookupEnv:         os.LookupEnv,
		ReadFile:          os.ReadFile,
		WriteFile:         os.WriteFile,
		OpenFile:          openFile,
		RemoveFile:        os.Remove,
		ReadDir:           os.ReadDir,
		Stat:              os.Stat,
		MkdirAll:          os.MkdirAll,
		Now:               time.Now,
		NewID:             uuid.NewV7,
		OpenStore:         store.Open,
		OpenStoreReadOnly: store.OpenReadOnly,
	})
	os.Exit(code)
}

func openFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(filepath.Clean(name), flag, perm)
}
