package render

import (
	"strings"
	"testing"
)

func TestMarkdownTableAlignedWithBorders(t *testing.T) {
	src := "Here you go:\n\n| Name | Qty | Note |\n|:-----|----:|:----:|\n| apple | 3 | **fresh** |\n| kiwi | 12 | `a\\|b` |\n\nDone."
	got := Markdown(src, Options{Width: 80})
	want := strings.Join([]string{
		"Here you go:",
		"",
		"┌───────┬─────┬───────┐",
		"│ Name  │ Qty │ Note  │",
		"├───────┼─────┼───────┤",
		"│ apple │   3 │ fresh │",
		"│ kiwi  │  12 │  a|b  │",
		"└───────┴─────┴───────┘",
		"",
		"Done.",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownTableWrapsToWidth(t *testing.T) {
	src := "| Key | Description |\n|---|---|\n| a | " + strings.Repeat("word ", 20) + "|\n"
	got := Markdown(src, Options{Width: 30})
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if w := strWidth(line); w > 30 {
			t.Errorf("line exceeds width 30 (%d): %q", w, line)
		}
		if !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "┌") &&
			!strings.HasPrefix(line, "├") && !strings.HasPrefix(line, "└") {
			t.Errorf("line not part of table: %q", line)
		}
	}
	if strings.Count(got, "word") != 20 {
		t.Errorf("cell content lost during wrap:\n%s", got)
	}
}

func TestMarkdownWideRunesAlign(t *testing.T) {
	got := Markdown("| a | b |\n|---|---|\n| 日本 | x |\n| ab | y |\n", Options{})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	w := strWidth(lines[0])
	for _, l := range lines {
		if strWidth(l) != w {
			t.Fatalf("misaligned rows:\n%s", got)
		}
	}
}

func TestMarkdownBlocks(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"heading", "## Summary", "Summary"},
		{"bullet", "  - item **one**", "  • item one"},
		{"quote", "> note", "│ note"},
		{"code fence verbatim", "```go\na | b | c\n**x**\n```", "```go\na | b | c\n**x**\n```"},
		{"pipes without separator are not a table", "a | b | c\nplain", "a | b | c\nplain"},
		{"unmatched markers kept", "2 ** 3 and `x", "2 ** 3 and `x"},
		{"rule", "---", strings.Repeat("─", 40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Markdown(tt.src, Options{}); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarkdownColor(t *testing.T) {
	got := Markdown("use **bold** and `code`", Options{Color: true})
	want := "use " + ansiBold + "bold" + ansiReset + " and " + ansiCode + "code" + ansiReset
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamMatchesWholeDocument(t *testing.T) {
	src := "# Title\n\nIntro text\n| h1 | h2 |\n|----|----|\n| a | b |\n| c | d |\nafter\n```\n| not | table |\n```\n- end"
	want := Markdown(src, Options{Width: 60})
	// Feed in awkward chunk sizes, as token streams arrive.
	for _, size := range []int{1, 2, 3, 7, 64} {
		var b strings.Builder
		s := NewStream(&b, Options{Width: 60})
		for i := 0; i < len(src); i += size {
			end := min(i+size, len(src))
			if _, err := s.WriteString(src[i:end]); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Flush(); err != nil {
			t.Fatal(err)
		}
		if b.String() != want {
			t.Fatalf("chunk=%d:\ngot:\n%s\nwant:\n%s", size, b.String(), want)
		}
	}
}

func TestStreamHoldsTableUntilComplete(t *testing.T) {
	var b strings.Builder
	s := NewStream(&b, Options{})
	_, _ = s.WriteString("text\n| a | b |\n|---|---|\n| 1 | 2 |\n")
	if b.String() != "text\n" {
		t.Fatalf("table emitted before it ended: %q", b.String())
	}
	_, _ = s.WriteString("next\n")
	if !strings.Contains(b.String(), "┌") || !strings.HasSuffix(b.String(), "next\n") {
		t.Fatalf("table not flushed when block ended: %q", b.String())
	}
}

func TestRawPassthrough(t *testing.T) {
	src := "| a | b |\n|---|---|\n| **1** | 2 |"
	if got := Markdown(src, Options{Raw: true}); got != src {
		t.Fatalf("raw mode altered output: %q", got)
	}
	var b strings.Builder
	s := NewStream(&b, Options{Raw: true})
	_, _ = s.WriteString("| a ")
	if b.String() != "| a " {
		t.Fatalf("raw stream buffered output: %q", b.String())
	}
}
