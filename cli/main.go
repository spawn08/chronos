// Chronos CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/spawn08/chronos/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
