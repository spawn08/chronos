// Package slack provides a Slack bot interface for Chronos agents.
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// MessageHandler processes incoming messages and returns a response.
type MessageHandler func(ctx context.Context, channel, user, text, threadTS string) (string, error)

// Bot is a Slack bot that receives messages and routes them to an agent.
type Bot struct {
	token      string
	signingKey string
	handler    MessageHandler
	// httpClient is used for PostMessage; nil means http.DefaultClient.
	httpClient *http.Client
	mu         sync.RWMutex
	server     *http.Server
}

// New creates a new Slack bot.
// token is the Slack Bot OAuth token.
// signingKey is the Slack signing secret for request verification.
// handler is called for each incoming message.
func New(token, signingKey string, handler MessageHandler) *Bot {
	return &Bot{
		token:      token,
		signingKey: signingKey,
		handler:    handler,
	}
}

// ServeHTTP handles incoming Slack Events API requests.
func (b *Bot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Parse the outer event wrapper
	var envelope struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Event     struct {
			Type     string `json:"type"`
			Channel  string `json:"channel"`
			User     string `json:"user"`
			Text     string `json:"text"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
			BotID    string `json:"bot_id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// URL verification challenge
	if envelope.Type == "url_verification" {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(envelope.Challenge))
		return
	}

	// Ignore bot messages to prevent loops
	if envelope.Event.BotID != "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Handle message events
	if envelope.Type == "event_callback" && envelope.Event.Type == "message" {
		go b.handleMessage(r.Context(), envelope.Event.Channel, envelope.Event.User,
			envelope.Event.Text, envelope.Event.ThreadTS)
	}

	w.WriteHeader(http.StatusOK)
}

func (b *Bot) handleMessage(ctx context.Context, channel, user, text, threadTS string) {
	response, err := b.handler(ctx, channel, user, text, threadTS)
	if err != nil {
		response = fmt.Sprintf("Error: %v", err)
	}
	if response == "" {
		return
	}

	// Reply in thread if the message was in a thread
	replyTS := threadTS

	b.PostMessage(ctx, channel, response, replyTS)
}

// PostMessage sends a message to a Slack channel.
func (b *Bot) PostMessage(ctx context.Context, channel, text, threadTS string) error {
	body := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}

	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/chat.postMessage", strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if !result.OK {
		return fmt.Errorf("slack post: %s", result.Error)
	}
	return nil
}

// Start begins serving the Slack Events API on the given address.
func (b *Bot) Start(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/slack/events", b)

	b.mu.Lock()
	b.server = &http.Server{Addr: addr, Handler: mux}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.Stop()
	}()

	return b.server.ListenAndServe()
}

// Stop gracefully shuts down the bot server.
func (b *Bot) Stop() error {
	b.mu.RLock()
	srv := b.server
	b.mu.RUnlock()
	if srv != nil {
		return srv.Close()
	}
	return nil
}
