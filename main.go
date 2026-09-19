// nibr is a local-first CLI for nibrunner: it runs on the same host as nibrunnerd and talks to it
// the only way nibrunnerd's own proxy code insists on: writing desired.json, atomically, as the
// operator already logged into this box. Nothing here is a client of a network API, because
// nibrunnerd deliberately exposes none for writes.
package main

import (
	"fmt"
	"os"

	"nibrunner-cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "nibr: %v\n", err)
		os.Exit(1)
	}
}
