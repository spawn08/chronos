package loaders

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// PDFLoader loads PDF files as knowledge documents by extracting text content.
// It uses a lightweight pure-Go text extraction approach that handles common
// PDF text streams without requiring external dependencies like poppler.
type PDFLoader struct {
	paths     []string
	chunkSize int
	overlap   int
}

// NewPDFLoader creates a loader for PDF files.
// chunkSize controls the maximum characters per document chunk (0 = no chunking).
// overlap controls how many characters overlap between consecutive chunks.
func NewPDFLoader(paths []string, chunkSize, overlap int) *PDFLoader {
	return &PDFLoader{
		paths:     paths,
		chunkSize: chunkSize,
		overlap:   overlap,
	}
}

// Load reads all PDF files and returns documents, optionally chunked.
func (l *PDFLoader) Load() ([]knowledge.Document, error) {
	var docs []knowledge.Document

	for _, p := range l.paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("pdf loader: reading %q: %w", p, err)
		}

		content, err := extractPDFText(data)
		if err != nil {
			return nil, fmt.Errorf("pdf loader: extracting text from %q: %w", p, err)
		}

		base := filepath.Base(p)

		if l.chunkSize <= 0 {
			docs = append(docs, knowledge.Document{
				ID:      pdfDocID(p, 0),
				Content: content,
				Metadata: map[string]any{
					"source": p,
					"name":   base,
					"type":   "pdf",
				},
			})
			continue
		}

		chunks := chunkText(content, l.chunkSize, l.overlap)
		for i, chunk := range chunks {
			docs = append(docs, knowledge.Document{
				ID:      pdfDocID(p, i),
				Content: chunk,
				Metadata: map[string]any{
					"source":    p,
					"name":      base,
					"type":      "pdf",
					"chunk_idx": i,
					"total":     len(chunks),
				},
			})
		}
	}

	return docs, nil
}

// extractPDFText performs a lightweight extraction of text from PDF data.
// It parses text between BT (begin text) and ET (end text) operators,
// extracting Tj and TJ string operands. This handles many common PDFs
// but may not extract text from complex layouts or scanned PDFs.
func extractPDFText(data []byte) (string, error) {
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return "", fmt.Errorf("not a valid PDF file")
	}

	var textParts []string

	// Strategy 1: Extract text from BT/ET blocks
	btRe := regexp.MustCompile(`(?s)BT\s(.*?)\sET`)
	blocks := btRe.FindAll(data, -1)

	// Match parenthesized strings in Tj/TJ operators
	tjRe := regexp.MustCompile(`\(([^)]*)\)`)

	for _, block := range blocks {
		matches := tjRe.FindAllSubmatch(block, -1)
		for _, m := range matches {
			text := decodePDFString(string(m[1]))
			if text = strings.TrimSpace(text); text != "" {
				textParts = append(textParts, text)
			}
		}
	}

	// Strategy 2: If BT/ET extraction yields nothing, try stream extraction
	if len(textParts) == 0 {
		textParts = extractFromStreams(data)
	}

	result := strings.Join(textParts, " ")
	// Clean up multiple spaces
	spaceRe := regexp.MustCompile(`\s+`)
	result = spaceRe.ReplaceAllString(result, " ")
	result = strings.TrimSpace(result)

	if result == "" {
		return "", fmt.Errorf("no extractable text found (scanned PDF or complex encoding)")
	}

	return result, nil
}

// extractFromStreams extracts readable ASCII text from PDF stream objects.
func extractFromStreams(data []byte) []string {
	var parts []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Look for parenthesized text strings
		tjRe := regexp.MustCompile(`\(([^)]+)\)`)
		matches := tjRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			text := decodePDFString(m[1])
			if isReadableText(text) {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
	}
	return parts
}

// decodePDFString handles basic PDF string escape sequences.
func decodePDFString(s string) string {
	r := strings.NewReplacer(
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\\`, `\`,
		`\(`, "(",
		`\)`, ")",
	)
	return r.Replace(s)
}

// isReadableText checks if a string contains mostly printable ASCII characters.
func isReadableText(s string) bool {
	if len(s) < 2 {
		return false
	}
	readable := 0
	for _, c := range s {
		if c >= 32 && c <= 126 {
			readable++
		}
	}
	return float64(readable)/float64(len(s)) > 0.7
}

func pdfDocID(path string, chunk int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", path, chunk)))
	return fmt.Sprintf("pdf-%s", strings.ToLower(fmt.Sprintf("%x", h[:8])))
}
