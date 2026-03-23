package redis

import (
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bulk string with JSON object",
			input: "$42\r\n{\"id\":\"s1\",\"agent_id\":\"a1\",\"status\":\"running\"}\r\n",
			want:  `{"id":"s1","agent_id":"a1","status":"running"}`,
		},
		{
			name:  "bulk string with JSON array",
			input: "$10\r\n[1,2,3]\r\n",
			want:  "[1,2,3]",
		},
		{
			name:  "nested JSON object",
			input: "$20\r\n{\"a\":{\"b\":\"c\"}}\r\n",
			want:  `{"a":{"b":"c"}}`,
		},
		{
			name:  "empty response",
			input: "",
			want:  "",
		},
		{
			name:  "nil response (no JSON)",
			input: "$-1\r\n",
			want:  "",
		},
		{
			name:  "error response",
			input: "-ERR something\r\n",
			want:  "",
		},
		{
			name:  "nested arrays",
			input: "$20\r\n{\"items\":[1,[2,3]]}\r\n",
			want:  `{"items":[1,[2,3]]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseScanResponse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCursor string
		wantKeys   []string
	}{
		{
			name:       "single key with cursor 0",
			input:      "*2\r\n$1\r\n0\r\n*1\r\n$20\r\nchronos:session:s1\r\n",
			wantCursor: "0",
			wantKeys:   []string{"chronos:session:s1"},
		},
		{
			name:       "multiple keys with non-zero cursor",
			input:      "*2\r\n$2\r\n15\r\n*2\r\n$20\r\nchronos:session:s1\r\n$20\r\nchronos:session:s2\r\n",
			wantCursor: "15",
			wantKeys:   []string{"chronos:session:s1", "chronos:session:s2"},
		},
		{
			name:       "no keys found",
			input:      "*2\r\n$1\r\n0\r\n*0\r\n",
			wantCursor: "0",
			wantKeys:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cursor, keys := parseScanResponse(tt.input)
			if cursor != tt.wantCursor {
				t.Errorf("cursor = %q, want %q", cursor, tt.wantCursor)
			}
			if len(keys) != len(tt.wantKeys) {
				t.Errorf("keys count = %d, want %d", len(keys), len(tt.wantKeys))
				return
			}
			for i, k := range keys {
				if k != tt.wantKeys[i] {
					t.Errorf("keys[%d] = %q, want %q", i, k, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestParseArrayResponse(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want []string
	}{
		{
			name: "two elements",
			resp: "*2\r\n$2\r\ns1\r\n$2\r\ns2\r\n",
			want: []string{"s1", "s2"},
		},
		{
			name: "empty array",
			resp: "*0\r\n",
			want: nil,
		},
		{
			name: "single element",
			resp: "*1\r\n$3\r\nabc\r\n",
			want: []string{"abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseArrayResponse(tt.resp)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestKeyFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) string
		id   string
		want string
	}{
		{"sessionKey", sessionKey, "s1", "chronos:session:s1"},
		{"memoryKey", memoryKey, "m1", "chronos:memory:m1"},
		{"auditKey", auditKey, "a1", "chronos:audit:a1"},
		{"traceKey", traceKey, "t1", "chronos:trace:t1"},
		{"eventKey", eventKey, "e1", "chronos:event:e1"},
		{"checkpointKey", checkpointKey, "cp1", "chronos:checkpoint:cp1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.id)
			if got != tt.want {
				t.Errorf("%s(%q) = %q, want %q", tt.name, tt.id, got, tt.want)
			}
		})
	}
}

func TestIndexKeyFunctions(t *testing.T) {
	if got := sessionIndexKey("agent1"); got != "chronos:idx:sessions:agent1" {
		t.Errorf("sessionIndexKey = %q", got)
	}
	if got := auditIndexKey("sess1"); got != "chronos:idx:audits:sess1" {
		t.Errorf("auditIndexKey = %q", got)
	}
	if got := traceIndexKey("sess1"); got != "chronos:idx:traces:sess1" {
		t.Errorf("traceIndexKey = %q", got)
	}
	if got := eventIndexKey("sess1"); got != "chronos:idx:events:sess1" {
		t.Errorf("eventIndexKey = %q", got)
	}
	if got := checkpointIndexKey("sess1"); got != "chronos:idx:checkpoints:sess1" {
		t.Errorf("checkpointIndexKey = %q", got)
	}
	if got := memoryIndexKey("agent1", "long_term"); got != "chronos:idx:memory:agent1:long_term" {
		t.Errorf("memoryIndexKey = %q", got)
	}
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.Storage = (*Store)(nil)
}
