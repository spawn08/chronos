package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spawn08/chronos/examples/internal/exampletest"
)

func TestMainCompletes(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	out := exampletest.RunWithStdoutCapture(t, main)
	exampletest.AssertOutputContains(t, out, "Result:")

	db := filepath.Join(dir, "quickstart.db")
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("expected sqlite file at %s: %v", db, err)
	}
}
