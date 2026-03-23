package redisvector

import (
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestParseSearchResponse(t *testing.T) {
	tests := []struct {
		name       string
		resp       string
		collection string
		wantCount  int
		wantIDs    []string
		wantScores []float32
	}{
		{
			name: "two results with scores",
			resp: "*5\r\n" +
				":2\r\n" +
				"$10\r\nmycol:doc1\r\n" +
				"*6\r\n" +
				"$7\r\ncontent\r\n" +
				"$11\r\nhello world\r\n" +
				"$8\r\nmetadata\r\n" +
				"$2\r\n{}\r\n" +
				"$16\r\n__vector_score\r\n" +
				"$3\r\n0.1\r\n" +
				"$10\r\nmycol:doc2\r\n" +
				"*6\r\n" +
				"$7\r\ncontent\r\n" +
				"$9\r\nsome text\r\n" +
				"$8\r\nmetadata\r\n" +
				"$17\r\n{\"key\":\"value\"}\r\n" +
				"$16\r\n__vector_score\r\n" +
				"$3\r\n0.3\r\n",
			collection: "mycol",
			wantCount:  2,
			wantIDs:    []string{"doc1", "doc2"},
			wantScores: []float32{0.9, 0.7},
		},
		{
			name:       "empty response",
			resp:       "",
			collection: "mycol",
			wantCount:  0,
		},
		{
			name:       "no results",
			resp:       "*1\r\n:0\r\n",
			collection: "mycol",
			wantCount:  0,
		},
		{
			name: "single result without score",
			resp: "*3\r\n" +
				":1\r\n" +
				"$10\r\nmycol:doc1\r\n" +
				"*4\r\n" +
				"$7\r\ncontent\r\n" +
				"$5\r\nhello\r\n" +
				"$8\r\nmetadata\r\n" +
				"$2\r\n{}\r\n",
			collection: "mycol",
			wantCount:  1,
			wantIDs:    []string{"doc1"},
		},
		{
			name: "result with metadata",
			resp: "*3\r\n" +
				":1\r\n" +
				"$11\r\ntestcol:id1\r\n" +
				"*4\r\n" +
				"$7\r\ncontent\r\n" +
				"$4\r\ntest\r\n" +
				"$8\r\nmetadata\r\n" +
				"$20\r\n{\"source\":\"web.pdf\"}\r\n",
			collection: "testcol",
			wantCount:  1,
			wantIDs:    []string{"id1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := ParseSearchResponse(tt.resp, tt.collection)
			if len(results) != tt.wantCount {
				t.Fatalf("got %d results, want %d", len(results), tt.wantCount)
			}
			for i, r := range results {
				if i < len(tt.wantIDs) && r.ID != tt.wantIDs[i] {
					t.Errorf("result[%d].ID = %q, want %q", i, r.ID, tt.wantIDs[i])
				}
				if i < len(tt.wantScores) {
					diff := r.Score - tt.wantScores[i]
					if diff < -0.05 || diff > 0.05 {
						t.Errorf("result[%d].Score = %f, want ~%f", i, r.Score, tt.wantScores[i])
					}
				}
			}
		})
	}
}

func TestParseSearchResponseContent(t *testing.T) {
	resp := "*3\r\n" +
		":1\r\n" +
		"$8\r\ncol:id1\r\n" +
		"*4\r\n" +
		"$7\r\ncontent\r\n" +
		"$13\r\nHello, world!\r\n" +
		"$8\r\nmetadata\r\n" +
		"$2\r\n{}\r\n"

	results := ParseSearchResponse(resp, "col")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "Hello, world!" {
		t.Errorf("content = %q, want %q", results[0].Content, "Hello, world!")
	}
	if results[0].ID != "id1" {
		t.Errorf("id = %q, want %q", results[0].ID, "id1")
	}
}

func TestParseSearchResponseMetadata(t *testing.T) {
	resp := "*3\r\n" +
		":1\r\n" +
		"$8\r\ncol:id1\r\n" +
		"*4\r\n" +
		"$7\r\ncontent\r\n" +
		"$4\r\ntest\r\n" +
		"$8\r\nmetadata\r\n" +
		"$23\r\n{\"source\":\"document.pdf\"}\r\n"

	results := ParseSearchResponse(resp, "col")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if src, ok := results[0].Metadata["source"]; !ok || src != "document.pdf" {
		t.Errorf("metadata[source] = %v, want %q", results[0].Metadata, "document.pdf")
	}
}

func TestFloatsToString(t *testing.T) {
	tests := []struct {
		name  string
		input []float32
		want  string
	}{
		{"empty", []float32{}, ""},
		{"single", []float32{1.5}, "1.5"},
		{"multiple", []float32{0.1, 0.2, 0.3}, "0.1,0.2,0.3"},
		{"integers", []float32{1, 2, 3}, "1,2,3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := floatsToString(tt.input)
			if got != tt.want {
				t.Errorf("floatsToString = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.VectorStore = (*Store)(nil)
}
