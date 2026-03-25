package main

import (
	"testing"

	"github.com/spawn08/chronos/examples/internal/exampletest"
)

func TestMainCompletes(t *testing.T) {
	out := exampletest.RunWithStdoutCapture(t, main)
	exampletest.AssertOutputContains(t, out, "Memory & Sessions example completed")
}
