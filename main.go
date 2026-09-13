// Command coroner is the coroner CLI. It hands the process's args, stdio, environment,
// clock and signals to cli.Run and exits with its code.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Restoring default signal handling once ctx is done lets a second Ctrl-C kill at once.
	context.AfterFunc(ctx, stop)

	code := cli.Run(ctx, cli.App{
		Args:      os.Args[1:],
		In:        os.Stdin,
		Out:       os.Stdout,
		Err:       os.Stderr,
		LookupEnv: os.LookupEnv,
		Now:       time.Now,
		OpenStore: store.Open,
	})
	os.Exit(code)
}
