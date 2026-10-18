// Command compatfixture writes a new coroner database holding timelines, events,
// annotations and sources. A release commits its output to internal/store/testdata/compat,
// where the store's compatibility spec opens it with every later build.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/macintacos/coroner/internal/tools/compatfixture/fixture"
)

func main() {
	path := flag.String("o", "", "path of the database to write; it must not exist yet")
	flag.Parse()

	if *path == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/tools/compatfixture -o <path>")
		os.Exit(2)
	}

	err := fixture.Write(context.Background(), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compatfixture:", err)
		os.Exit(1)
	}
}
