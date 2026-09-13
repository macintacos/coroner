// Command coroner is the coroner CLI.
package main

import (
	"os"

	"github.com/macintacos/coroner/cmd"
)

func main() {
	if cmd.NewRoot().Execute() != nil {
		os.Exit(1)
	}
}
