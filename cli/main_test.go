package main

import (
	"testing"
)

func TestMainPackageCompiles(t *testing.T) {
	// Validates that the cli main package compiles and imports resolve.
	// The actual main() calls cmd.Execute() which is thoroughly tested in cli/cmd.
}
