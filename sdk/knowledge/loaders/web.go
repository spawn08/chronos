package loaders

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// WebLoader loads web pages as knowledge documents by scraping URL content
// and extracting readable text. It strips HTML tags and normalizes whitespace.
type WebLoader struct {
	urls      []string
	chunkSize int
	overlap   int
	client    *http.Client
}

// NewWebLoader creates a loader for web pages.
// chunkSize controls the maximum characters per document chunk (0 = no chunking).
// overlap controls how many characters overlap between consecutive chunks.
func NewWebLoader(urls []string, chunkSize, overlap int) *WebLoader {
	return &WebLoader{
		urls:      urls,
		chunkSize: chunkSize,
		overlap:   overlap,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithTimeout sets a custom HTTP timeout for the loader.
func (l *WebLoader) WithTimeout(timeout time.Duration) *WebLoader {
	l.client.Timeout = timeout
	return l
}

// Load fetches all URLs and returns documents, optionally chunked.
func (l *WebLoader) Load() ([]knowledge.Document, error) {
	var docs []knowledge.Document

	for _, u := range l.urls {
		content, title, err := l.fetchAndExtract(u)
		if err != nil {
			return nil, fmt.Errorf("web loader: fetching %q: %w", u, err)
		}

		if l.chunkSize <= 0 {
			docs = append(docs, knowledge.Document{
				ID:      webDocID(u, 0),
				Content: content,
				Metadata: map[string]any{
					"source": u,
					"title":  title,
					"type":   "web",
				},
			})
			continue
		}

		chunks := chunkText(content, l.chunkSize, l.overlap)
		for i, chunk := range chunks {
			docs = append(docs, knowledge.Document{
				ID:      webDocID(u, i),
				Content: chunk,
				Metadata: map[string]any{
					"source":    u,
					"title":     title,
					"type":      "web",
					"chunk_idx": i,
					"total":     len(chunks),
				},
			})
		}
	}

	return docs, nil
}

// fetchAndExtract fetches a URL and extracts readable text content.
func (l *WebLoader) fetchAndExtract(url string) (content, title string, err error) {
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Chronos/1.0 (Knowledge Loader)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetching: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB max
	if err != nil {
		return "", "", fmt.Errorf("reading body: %w", err)
	}

	html := string(body)

	// Extract title
	title = extractTitle(html)

	// Extract text content
	content = extractText(html)

	if content == "" {
		return "", title, fmt.Errorf("no extractable text content")
	}

	return content, title, nil
}

// extractTitle extracts the <title> tag content from HTML.
func extractTitle(html string) string {
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	m := re.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(stripTags(m[1]))
	}
	return ""
}

// extractText extracts readable text from HTML by removing scripts, styles,
// and tags, then normalizing whitespace.
func extractText(html string) string {
	// Remove script and style blocks
	text := html
	for _, tag := range []string{"script", "style", "noscript", "iframe"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		text = re.ReplaceAllString(text, "")
	}

	// Remove HTML comments
	commentRe := regexp.MustCompile(`(?s)<!--.*?-->`)
	text = commentRe.ReplaceAllString(text, "")

	// Replace block-level elements with newlines
	blockRe := regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr|br|hr)[^>]*>`)
	text = blockRe.ReplaceAllString(text, "\n")

	// Strip remaining tags
	text = stripTags(text)

	// Decode common HTML entities
	text = decodeHTMLEntities(text)

	// Normalize whitespace
	spaceRe := regexp.MustCompile(`[ \t]+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// Normalize newlines
	nlRe := regexp.MustCompile(`\n{3,}`)
	text = nlRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// stripTags removes all HTML tags from a string.
func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// decodeHTMLEntities replaces common HTML entities with their characters.
func decodeHTMLEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
		"&mdash;", "—",
		"&ndash;", "–",
		"&hellip;", "…",
		"&copy;", "©",
		"&reg;", "®",
		"&trade;", "™",
	)
	return r.Replace(s)
}

func webDocID(url string, chunk int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", url, chunk)))
	return fmt.Sprintf("web-%s", strings.ToLower(fmt.Sprintf("%x", h[:8])))
}
