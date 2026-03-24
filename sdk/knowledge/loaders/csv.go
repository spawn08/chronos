package loaders

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// CSVLoader loads CSV files as knowledge documents (one document per row).
type CSVLoader struct {
	paths     []string
	hasHeader bool
}

// NewCSVLoader creates a loader for CSV files.
// If hasHeader is true, the first row is treated as column names and used to label fields.
func NewCSVLoader(paths []string, hasHeader bool) *CSVLoader {
	return &CSVLoader{paths: paths, hasHeader: hasHeader}
}

// Load reads all CSV files and returns one document per row.
func (l *CSVLoader) Load() ([]knowledge.Document, error) {
	var docs []knowledge.Document

	for _, p := range l.paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("csv loader: opening %q: %w", p, err)
		}
		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("csv loader: reading %q: %w", p, err)
		}

		if len(records) == 0 {
			continue
		}

		var headers []string
		startIdx := 0
		if l.hasHeader && len(records) > 0 {
			headers = records[0]
			startIdx = 1
		}

		for i := startIdx; i < len(records); i++ {
			row := records[i]
			var content strings.Builder
			for j, val := range row {
				if j < len(headers) {
					content.WriteString(headers[j])
					content.WriteString(": ")
				}
				content.WriteString(val)
				if j < len(row)-1 {
					content.WriteString(", ")
				}
			}
			docs = append(docs, knowledge.Document{
				ID:      docID(p, i-startIdx),
				Content: content.String(),
				Metadata: map[string]any{
					"source": p,
					"type":   "csv",
					"row":    i,
				},
			})
		}
	}
	return docs, nil
}
