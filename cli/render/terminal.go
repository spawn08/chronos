package render

import (
	"os"

	"golang.org/x/term"
)

// ForFile returns Options suited to f: full rendering when f is an
// interactive terminal, raw passthrough otherwise (pipes, redirects, tests),
// so scripted consumers still receive the model's exact Markdown. Colour
// honours NO_COLOR and TERM=dumb; CHRONOS_RAW_OUTPUT=1 disables rendering.
func ForFile(f *os.File) Options {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) || os.Getenv("CHRONOS_RAW_OUTPUT") == "1" {
		return Options{Raw: true}
	}
	opts := Options{
		Color: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
	}
	if w, _, err := term.GetSize(fd); err == nil {
		opts.Width = w
	}
	return opts
}

// Stdout returns a Stream that renders to os.Stdout.
func Stdout() *Stream {
	return NewStream(os.Stdout, ForFile(os.Stdout))
}
