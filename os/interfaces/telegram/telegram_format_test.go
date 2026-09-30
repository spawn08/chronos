package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage_FallsBackToPlainTextOnParseError(t *testing.T) {
	var modes []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		modes = append(modes, body["parse_mode"])
		if body["parse_mode"] != nil {
			fmt.Fprint(w, `{"ok":false,"description":"Bad Request: can't parse entities: Can't find end of the entity starting at byte offset 42"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	if err := b.SendMessage(context.Background(), 1, "| a_b | c* |\n|---|---|\n| 1 | 2 |"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(modes) != 2 || modes[0] != "Markdown" || modes[1] != nil {
		t.Fatalf("parse modes sent = %v, want [Markdown <nil>]", modes)
	}
}

func TestSendMessage_NoFallbackForOtherErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprint(w, `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	if err := b.SendMessage(context.Background(), 1, "hi"); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
