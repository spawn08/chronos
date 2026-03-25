package loaders

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// JSONLoader loads JSON files as knowledge documents.
// Supports both JSON arrays (one document per element) and single objects (one document).
type JSONLoader struct {
	paths    []string
	jmesPath string //nolint:unused // reserved for future JMESPath filtering
}

// NewJSONLoader creates a loader for JSON files.
func NewJSONLoader(paths []string) *JSONLoader {
	return &JSONLoader{paths: paths}
}

// Load reads all JSON files and returns documents.
func (l *JSONLoader) Load() ([]knowledge.Document, error) {
	var docs []knowledge.Document

	for _, p := range l.paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("json loader: reading %q: %w", p, err)
		}

		var raw any
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("json loader: parsing %q: %w", p, err)
		}

		switch v := raw.(type) {
		case []any:
			for i, item := range v {
				content, _ := json.Marshal(item)
				docs = append(docs, knowledge.Document{
					ID:      docID(p, i),
					Content: string(content),
					Metadata: map[string]any{
						"source": p,
						"type":   "json",
						"index":  i,
					},
				})
			}
		case map[string]any:
			content, _ := json.Marshal(v)
			docs = append(docs, knowledge.Document{
				ID:      docID(p, 0),
				Content: string(content),
				Metadata: map[string]any{
					"source": p,
					"type":   "json",
				},
			})
		default:
			content, _ := json.Marshal(v)
			docs = append(docs, knowledge.Document{
				ID:      docID(p, 0),
				Content: string(content),
				Metadata: map[string]any{
					"source": p,
					"type":   "json",
				},
			})
		}
	}
	return docs, nil
}
