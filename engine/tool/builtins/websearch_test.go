package builtins

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebSearch_MissingQuery(t *testing.T) {
	ws := NewWebSearchTool(5*time.Second, 5)
	_, err := ws.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	ws := NewWebSearchTool(5*time.Second, 5)
	_, err := ws.Handler(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearch_NonStringQuery(t *testing.T) {
	ws := NewWebSearchTool(0, 0)
	_, err := ws.Handler(context.Background(), map[string]any{"query": 123})
	if err == nil {
		t.Fatal("expected error for non-string query")
	}
}

func TestWebSearch_Definition(t *testing.T) {
	ws := NewWebSearchTool(0, 0)
	if ws.Name != "web_search" {
		t.Errorf("name = %q, want web_search", ws.Name)
	}
	if ws.Description == "" {
		t.Error("description should not be empty")
	}
}

func TestWebSearchCustom_MissingQuery(t *testing.T) {
	ws := NewWebSearchToolWithEngine("", 0)
	_, err := ws.Handler(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearchCustom_Definition(t *testing.T) {
	ws := NewWebSearchToolWithEngine("", 0)
	if ws.Name != "web_search_custom" {
		t.Errorf("name = %q, want web_search_custom", ws.Name)
	}
}

func TestWebSearchCustom_WithHTTPServer(t *testing.T) {
	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": "ok"}`)
	}))
	defer testSrv.Close()

	ws := NewWebSearchToolWithEngine(testSrv.URL+"/search?q=%s", 5*time.Second)
	result, err := ws.Handler(context.Background(), map[string]any{"query": "test query"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result")
	}
	if m["query"] != "test query" {
		t.Errorf("query=%v", m["query"])
	}
}

func TestWebSearchCustom_URLWithoutPercent(t *testing.T) {
	// When engine URL doesn't have %s, it should append ?q=%s
	ws := NewWebSearchToolWithEngine("http://localhost:12345/search", 0)
	if ws == nil {
		t.Fatal("expected non-nil definition")
	}
}

func TestNewWebSearchTool_WithHTTPServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Abstract":"test abstract","AbstractSource":"Wikipedia","AbstractURL":"http://example.com","RelatedTopics":[{"Text":"topic 1","FirstURL":"http://example.com/1"}]}`)
	}))
	defer srv.Close()

	// Override the DuckDuckGo URL by using the WithEngine version
	ws := NewWebSearchToolWithEngine(srv.URL+"?q=%s", 5*time.Second)
	result, err := ws.Handler(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestWebSearchTool_MockDDGResponse_Table(t *testing.T) {
	longText := strings.Repeat("x", 120)
	tests := []struct {
		name       string
		jsonBody   string
		maxResults int
		wantCount  int
		check      func(t *testing.T, m map[string]any)
	}{
		{
			name: "abstract_answer_and_topics",
			jsonBody: fmt.Sprintf(`{"Abstract":"abs text","AbstractSource":"Wiki","AbstractURL":"http://w","Answer":"42","RelatedTopics":[`+
				`{"Text":"short","FirstURL":"http://a"},`+
				`{"Text":"","FirstURL":"http://skip"},`+
				`{"Text":"%s","FirstURL":"http://long"}]}`, longText),
			maxResults: 10,
			wantCount:  4,
			check: func(t *testing.T, m map[string]any) {
				res := m["results"].([]map[string]string)
				if res[0]["snippet"] != "abs text" || res[0]["title"] != "Wiki" {
					t.Errorf("abstract entry: %+v", res[0])
				}
				if res[1]["title"] != "Instant Answer" || res[1]["snippet"] != "42" {
					t.Errorf("answer entry: %+v", res[1])
				}
				if len(res[3]["title"]) != 100 {
					t.Errorf("title truncated to 100, got len=%d", len(res[3]["title"]))
				}
			},
		},
		{
			name:       "empty_results",
			jsonBody:   `{}`,
			maxResults: 5,
			wantCount:  0,
			check: func(t *testing.T, m map[string]any) {
				if m["count"].(int) != 0 {
					t.Errorf("count=%v", m["count"])
				}
			},
		},
		{
			name: "max_results_caps_related",
			jsonBody: `{
				"Abstract":"only abstract",
				"AbstractSource":"S",
				"AbstractURL":"u",
				"RelatedTopics":[
					{"Text":"t1","FirstURL":"a"},
					{"Text":"t2","FirstURL":"b"},
					{"Text":"t3","FirstURL":"c"}
				]
			}`,
			maxResults: 2,
			wantCount:  2,
			check: func(t *testing.T, m map[string]any) {
				res := m["results"].([]map[string]string)
				if len(res) != 2 {
					t.Fatalf("len=%d", len(res))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tt.jsonBody)
			}))
			defer srv.Close()

			client := &http.Client{Timeout: 5 * time.Second}
			def := webSearchTool(client, tt.maxResults, srv.URL+"?q=%s&format=json&no_html=1&skip_disambig=1")
			out, err := def.Handler(context.Background(), map[string]any{"query": "q"})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			m := out.(map[string]any)
			if m["count"].(int) != tt.wantCount {
				t.Errorf("count=%v, want %d", m["count"], tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestWebSearchTool_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	def := webSearchTool(client, 5, srv.URL+"?q=%s")
	_, err := def.Handler(context.Background(), map[string]any{"query": "x"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestNewWebSearchToolWithEngine_EdgeCases(t *testing.T) {
	t.Run("custom_engine_json_handler", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ok":true}`)
		}))
		defer srv.Close()

		ws := NewWebSearchToolWithEngine(srv.URL+"?q=%s&format=json&no_html=1", time.Second)
		_, err := ws.Handler(context.Background(), map[string]any{"query": "z"})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
	})

	t.Run("engine_without_percent_appends_query", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery == "" {
				t.Error("expected query string on request")
			}
			fmt.Fprint(w, `{"body":"ok"}`)
		}))
		defer srv.Close()

		ws := NewWebSearchToolWithEngine(srv.URL+"/path", time.Second)
		res, err := ws.Handler(context.Background(), map[string]any{"query": "hello"})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		m := res.(map[string]any)
		if m["status_code"].(int) != http.StatusOK {
			t.Errorf("status=%v", m["status_code"])
		}
	})

	t.Run("non_string_query_custom", func(t *testing.T) {
		ws := NewWebSearchToolWithEngine("http://example.com/%s", time.Second)
		_, err := ws.Handler(context.Background(), map[string]any{"query": 1})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
