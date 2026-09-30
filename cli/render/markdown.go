// Package render formats model output (Markdown) for display in a terminal.
//
// Models routinely answer with Markdown tables, headings, lists, and code
// blocks. Printed raw, wide tables soft-wrap mid-row and their pipes no
// longer line up, which garbles the output. Stream renders Markdown line by
// line as tokens arrive: tables are buffered until complete, then drawn with
// aligned, width-aware box borders (wrapping long cells to fit the terminal);
// headings, bullets, blockquotes, and inline bold/code are styled; fenced code
// is passed through verbatim.
package render

import (
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// ANSI styles used when colour is enabled.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiUnder = "\x1b[4m"
	ansiCode  = "\x1b[36m"
)

// minColWidth is the narrowest a table column is shrunk to when fitting the
// terminal width; below this, cells become unreadable, so the table overflows.
const minColWidth = 6

// Options configures rendering.
type Options struct {
	// Width is the terminal width in columns; <= 0 disables table fitting.
	Width int `json:"width"`
	// Color enables ANSI styling. When false, Markdown markers are still
	// removed and tables are still aligned, but no escape codes are emitted.
	Color bool `json:"color"`
	// Raw disables rendering entirely: input is written through unchanged.
	// Use it when output is not a terminal (pipes, files, tests).
	Raw bool `json:"raw"`
}

// Markdown renders a complete Markdown document.
func Markdown(src string, opts Options) string {
	if opts.Raw {
		return src
	}
	var b strings.Builder
	s := NewStream(&b, opts)
	_, _ = s.Write([]byte(src))
	_ = s.Flush()
	return b.String()
}

// Stream incrementally renders Markdown written to it. It is not safe for
// concurrent use. Call Flush when the response ends.
type Stream struct {
	w    io.Writer
	opts Options
	err  error
	re   *patterns

	partial   string  // incomplete trailing line
	inFence   bool    // inside a ``` / ~~~ code block
	fence     string  // the fence marker that opened the block
	candidate *string // possible table header awaiting its separator line
	table     *tableBuf
}

type tableBuf struct {
	aligns []byte // 'l', 'c', or 'r' per column
	rows   [][]string
}

// NewStream returns a Stream writing rendered output to w.
func NewStream(w io.Writer, opts Options) *Stream {
	return &Stream{w: w, opts: opts, re: newPatterns()}
}

// Write consumes a chunk of Markdown. Complete lines are rendered
// immediately except table rows, which are held until the table ends.
func (s *Stream) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.opts.Raw {
		_, s.err = s.w.Write(p)
		return len(p), s.err
	}
	data := s.partial + string(p)
	for {
		i := strings.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		s.line(strings.TrimSuffix(data[:i], "\r"))
		data = data[i+1:]
	}
	s.partial = data
	return len(p), s.err
}

// WriteString is a convenience wrapper around Write.
func (s *Stream) WriteString(str string) (int, error) { return s.Write([]byte(str)) }

// Flush renders any buffered partial line, pending header, or table. It does
// not add a trailing newline after an unterminated final line.
func (s *Stream) Flush() error {
	if s.opts.Raw || s.err != nil {
		return s.err
	}
	last := s.partial
	s.partial = ""
	if last != "" {
		// Render the final line through the normal path, then drop the
		// newline that path appends so callers control the line ending.
		var tmp strings.Builder
		saved := s.w
		s.w = &tmp
		s.line(last)
		s.endBlocks()
		s.w = saved
		s.emitRaw(strings.TrimSuffix(tmp.String(), "\n"))
		return s.err
	}
	s.endBlocks()
	return s.err
}

func (s *Stream) endBlocks() {
	if s.candidate != nil {
		c := *s.candidate
		s.candidate = nil
		s.emit(s.renderLine(c))
	}
	if s.table != nil {
		s.flushTable()
	}
}

// patterns holds the compiled line-level Markdown matchers. They live on
// each Stream (not package-level) per the project's no-global-state rule.
type patterns struct {
	fence, heading, bullet, quote, rule, sepCell, ansi *regexp.Regexp
}

func newPatterns() *patterns {
	return &patterns{
		fence:   regexp.MustCompile("^\\s*(```+|~~~+)"),
		heading: regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`),
		bullet:  regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`),
		quote:   regexp.MustCompile(`^\s*>\s?(.*)$`),
		rule:    regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`),
		sepCell: regexp.MustCompile(`^\s*:?-+:?\s*$`),
		ansi:    regexp.MustCompile("\x1b\\[[0-9;]*m"),
	}
}

func (s *Stream) line(l string) {
	if s.inFence {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, s.fence) && strings.Trim(t, s.fence[:1]) == "" {
			s.inFence = false
			s.emit(s.style(ansiDim, l))
			return
		}
		s.emit(l)
		return
	}

	if s.candidate != nil {
		header := *s.candidate
		s.candidate = nil
		if aligns, ok := s.parseSeparator(l); ok {
			s.table = &tableBuf{aligns: aligns, rows: [][]string{splitRow(header)}}
			return
		}
		s.emit(s.renderLine(header))
	}

	if s.table != nil {
		if isTableRow(l) {
			s.table.rows = append(s.table.rows, splitRow(l))
			return
		}
		s.flushTable()
	}

	if m := s.re.fence.FindStringSubmatch(l); m != nil {
		s.inFence = true
		s.fence = m[1]
		s.emit(s.style(ansiDim, l))
		return
	}
	if isTableRow(l) {
		c := l
		s.candidate = &c
		return
	}
	s.emit(s.renderLine(l))
}

// renderLine styles a single non-table, non-code line.
func (s *Stream) renderLine(l string) string {
	if m := s.re.heading.FindStringSubmatch(l); m != nil {
		text := s.inline(m[2])
		if len(m[1]) == 1 {
			return s.style(ansiBold+ansiUnder, s.re.ansi.ReplaceAllString(text, ""))
		}
		return s.style(ansiBold, s.re.ansi.ReplaceAllString(text, ""))
	}
	if s.re.rule.MatchString(l) {
		n := 40
		if s.opts.Width > 0 && s.opts.Width < n {
			n = s.opts.Width
		}
		return s.style(ansiDim, strings.Repeat("─", n))
	}
	if m := s.re.bullet.FindStringSubmatch(l); m != nil {
		return m[1] + "• " + s.inline(m[2])
	}
	if m := s.re.quote.FindStringSubmatch(l); m != nil {
		return s.style(ansiDim, "│ ") + s.inline(m[1])
	}
	return s.inline(l)
}

// --- inline formatting ---

type segment struct {
	text  string
	style string // "", ansiBold, or ansiCode
}

// parseInline splits text into styled segments, handling `code` and
// **bold** / __bold__. Unmatched markers are kept literally.
func parseInline(text string) []segment {
	var segs []segment
	var cur strings.Builder
	flush := func(style string) {
		if cur.Len() > 0 {
			segs = append(segs, segment{text: cur.String(), style: style})
			cur.Reset()
		}
	}
	for i := 0; i < len(text); {
		switch {
		case text[i] == '`':
			if j := strings.IndexByte(text[i+1:], '`'); j >= 0 {
				flush("")
				segs = append(segs, segment{text: text[i+1 : i+1+j], style: ansiCode})
				i += j + 2
				continue
			}
		case strings.HasPrefix(text[i:], "**") || strings.HasPrefix(text[i:], "__"):
			marker := text[i : i+2]
			if j := strings.Index(text[i+2:], marker); j > 0 {
				flush("")
				for _, inner := range parseInline(text[i+2 : i+2+j]) {
					if inner.style == "" {
						inner.style = ansiBold
					}
					segs = append(segs, inner)
				}
				i += j + 4
				continue
			}
		}
		cur.WriteByte(text[i])
		i++
	}
	flush("")
	return segs
}

func (s *Stream) inline(text string) string {
	var b strings.Builder
	for _, seg := range parseInline(text) {
		b.WriteString(s.style(seg.style, seg.text))
	}
	return b.String()
}

func (s *Stream) style(code, text string) string {
	if !s.opts.Color || code == "" || text == "" {
		return text
	}
	return code + text + ansiReset
}

// --- tables ---

func isTableRow(l string) bool {
	t := strings.TrimSpace(l)
	return strings.Count(t, "|") >= 1 && (strings.HasPrefix(t, "|") || strings.Count(t, "|") >= 2)
}

// parseSeparator recognises a table delimiter row such as |---|:--:|---:|.
func (s *Stream) parseSeparator(l string) ([]byte, bool) {
	t := strings.TrimSpace(l)
	if !strings.Contains(t, "|") || !strings.Contains(t, "-") {
		return nil, false
	}
	cells := splitRow(t)
	if len(cells) == 0 {
		return nil, false
	}
	aligns := make([]byte, len(cells))
	for i, c := range cells {
		if !s.re.sepCell.MatchString(c) {
			return nil, false
		}
		c = strings.TrimSpace(c)
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			aligns[i] = 'c'
		case strings.HasSuffix(c, ":"):
			aligns[i] = 'r'
		default:
			aligns[i] = 'l'
		}
	}
	return aligns, true
}

// splitRow splits a table row on unescaped pipes outside inline code.
func splitRow(l string) []string {
	t := strings.TrimSpace(l)
	t = strings.TrimPrefix(t, "|")
	if strings.HasSuffix(t, "|") && !strings.HasSuffix(t, `\|`) {
		t = t[:len(t)-1]
	}
	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c == '\\' && i+1 < len(t) && t[i+1] == '|':
			cur.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			cur.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

func (s *Stream) flushTable() {
	tb := s.table
	s.table = nil
	cols := len(tb.aligns)
	for _, r := range tb.rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	// Parse every cell into styled segments and measure its natural width.
	cells := make([][][]segment, len(tb.rows))
	widths := make([]int, cols)
	for i, r := range tb.rows {
		cells[i] = make([][]segment, cols)
		for j := 0; j < cols; j++ {
			var text string
			if j < len(r) {
				text = strings.ReplaceAll(r[j], "<br>", " ")
			}
			segs := parseInline(text)
			if i == 0 {
				for k := range segs {
					segs[k].style = ansiBold
				}
			}
			cells[i][j] = segs
			if w := segsWidth(segs); w > widths[j] {
				widths[j] = w
			}
		}
	}
	for j := range widths {
		if widths[j] < 1 {
			widths[j] = 1
		}
	}
	fitWidths(widths, s.opts.Width)

	border := func(l, m, r string) string {
		parts := make([]string, cols)
		for j, w := range widths {
			parts[j] = strings.Repeat("─", w+2)
		}
		return s.style(ansiDim, l+strings.Join(parts, m)+r)
	}
	bar := s.style(ansiDim, "│")

	s.emit(border("┌", "┬", "┐"))
	for i := range cells {
		wrapped := make([][][]segment, cols)
		height := 1
		for j := 0; j < cols; j++ {
			wrapped[j] = wrapSegments(cells[i][j], widths[j])
			if len(wrapped[j]) > height {
				height = len(wrapped[j])
			}
		}
		for line := 0; line < height; line++ {
			var b strings.Builder
			b.WriteString(bar)
			for j := 0; j < cols; j++ {
				var segs []segment
				if line < len(wrapped[j]) {
					segs = wrapped[j][line]
				}
				align := byte('l')
				if j < len(tb.aligns) {
					align = tb.aligns[j]
				}
				pad := widths[j] - segsWidth(segs)
				if pad < 0 {
					pad = 0
				}
				left, right := 0, pad
				switch align {
				case 'r':
					left, right = pad, 0
				case 'c':
					left, right = pad/2, pad-pad/2
				}
				b.WriteString(" " + strings.Repeat(" ", left))
				for _, sg := range segs {
					b.WriteString(s.style(sg.style, sg.text))
				}
				b.WriteString(strings.Repeat(" ", right) + " ")
				b.WriteString(bar)
			}
			s.emit(b.String())
		}
		if i == 0 && len(cells) > 1 {
			s.emit(border("├", "┼", "┤"))
		}
	}
	s.emit(border("└", "┴", "┘"))
}

// fitWidths shrinks the widest columns until the table (content plus
// "│ " / " │" borders) fits within total columns, never below minColWidth.
func fitWidths(widths []int, total int) {
	if total <= 0 {
		return
	}
	avail := total - 3*len(widths) - 1
	for {
		sum, widest := 0, 0
		for j, w := range widths {
			sum += w
			if w > widths[widest] {
				widest = j
			}
		}
		if sum <= avail || widths[widest] <= minColWidth {
			return
		}
		widths[widest]--
	}
}

// wrapSegments word-wraps styled segments to at most max display columns
// per line, hard-splitting words longer than a line.
func wrapSegments(segs []segment, max int) [][]segment {
	type word struct {
		text  string
		style string
		space bool // preceded by whitespace
	}
	var words []word
	space := false
	for _, sg := range segs {
		start := -1
		for i, r := range sg.text {
			if unicode.IsSpace(r) {
				if start >= 0 {
					words = append(words, word{sg.text[start:i], sg.style, space})
					start = -1
				}
				space = true
				continue
			}
			if start < 0 {
				start = i
			}
		}
		if start >= 0 {
			words = append(words, word{sg.text[start:], sg.style, space})
			space = false
		}
	}

	var lines [][]segment
	var cur []segment
	curW := 0
	push := func() {
		lines = append(lines, cur)
		cur, curW = nil, 0
	}
	for _, w := range words {
		for w.text != "" {
			ww := strWidth(w.text)
			sep := 0
			if curW > 0 && w.space {
				sep = 1
			}
			if curW+sep+ww <= max {
				if sep == 1 {
					cur = append(cur, segment{text: " "})
				}
				cur = append(cur, segment{text: w.text, style: w.style})
				curW += sep + ww
				w.text = ""
				break
			}
			if curW > 0 {
				push()
				continue
			}
			// Word longer than a whole line: split it.
			head, tail := splitAtWidth(w.text, max)
			cur = append(cur, segment{text: head, style: w.style})
			curW = strWidth(head)
			push()
			w.text, w.space = tail, false
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

func splitAtWidth(s string, max int) (string, string) {
	w := 0
	for i, r := range s {
		rw := runeWidth(r)
		if w+rw > max && i > 0 {
			return s[:i], s[i:]
		}
		w += rw
	}
	return s, ""
}

func segsWidth(segs []segment) int {
	n := 0
	for _, sg := range segs {
		n += strWidth(sg.text)
	}
	return n
}

// strWidth returns the display width of s in terminal columns.
func strWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == utf8.RuneError || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == '\u200d' || r == '\ufe0f':
		return 0
	case r >= 0x1F300 && r <= 0x1FAFF: // emoji & pictographs render double-width
		return 2
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// --- output ---

func (s *Stream) emit(line string) { s.emitRaw(line + "\n") }

func (s *Stream) emitRaw(text string) {
	if s.err != nil || text == "" {
		return
	}
	_, s.err = io.WriteString(s.w, text)
}
