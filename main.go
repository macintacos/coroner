// Command coroner is the coroner CLI. It hands the process's args, stdio and whether each
// output is a terminal, environment, file reads, clock, build version and signals to
// cli.Run and exits with its code.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Restoring default signal handling once ctx is done lets a second Ctrl-C kill at once.
	context.AfterFunc(ctx, stop)

	build, _ := debug.ReadBuildInfo()
	code := cli.Run(ctx, cli.App{
		Args:      os.Args[1:],
		In:        os.Stdin,
		Out:       os.Stdout,
		Err:       os.Stderr,
		OutIsTTY:  isatty.IsTerminal(os.Stdout.Fd()),
		ErrIsTTY:  isatty.IsTerminal(os.Stderr.Fd()),
		Version:   build.Main.Version,
		LookupEnv: os.LookupEnv,
		ReadFile:  os.ReadFile,
		Now:       time.Now,
		OpenStore: store.Open,
	})
	os.Exit(code)
}
