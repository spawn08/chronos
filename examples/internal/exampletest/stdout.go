// Package exampletest provides helpers for tests under examples/.
package exampletest

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// RunWithStdoutCapture runs fn with os.Stdout captured and returns the combined output.
func RunWithStdoutCapture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// AssertOutputContains fails the test if out does not contain substr.
func AssertOutputContains(t *testing.T, out, substr string) {
	t.Helper()
	if strings.Contains(out, substr) {
		return
	}
	preview := out
	const max = 800
	if len(preview) > max {
		preview = preview[:max] + "..."
	}
	t.Fatalf("expected output to contain %q; preview:\n%s", substr, preview)
}
