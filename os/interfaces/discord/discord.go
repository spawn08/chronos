// Package discord provides a Discord bot interface for Chronos agents.
package discord

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
type MessageHandler func(ctx context.Context, channelID, userID, content string) (string, error)

// Bot is a Discord bot that listens for messages and routes them to an agent.
type Bot struct {
	token   string
	handler MessageHandler
	// httpClient is used for SendMessage; nil means http.DefaultClient.
	httpClient *http.Client
	mu         sync.RWMutex
	stopCh     chan struct{}
}

// New creates a new Discord bot.
// token is the Discord Bot token.
// handler is called for each incoming message.
func New(token string, handler MessageHandler) *Bot {
	return &Bot{
		token:   token,
		handler: handler,
		stopCh:  make(chan struct{}),
	}
}

// SendMessage sends a message to a Discord channel.
func (b *Bot) SendMessage(ctx context.Context, channelID, content string) error {
	body := map[string]any{
		"content": content,
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("discord send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+b.token)

	client := b.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("discord send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord send: HTTP %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

// HandleInteraction processes Discord Gateway interaction events.
// This is designed to be called from a webhook handler.
func (b *Bot) HandleInteraction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var interaction struct {
		Type int `json:"type"`
		Data struct {
			Name    string `json:"name"`
			Options []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"options"`
		} `json:"data"`
		ChannelID string `json:"channel_id"`
		Member    struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"member"`
	}
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Ping (type 1) — respond with Pong
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"type": 1})
		return
	}

	// Application command (type 2)
	if interaction.Type == 2 {
		content := interaction.Data.Name
		for _, opt := range interaction.Data.Options {
			content += " " + opt.Value
		}

		go func() {
			response, err := b.handler(r.Context(), interaction.ChannelID,
				interaction.Member.User.ID, content)
			if err != nil {
				response = fmt.Sprintf("Error: %v", err)
			}
			if response != "" {
				b.SendMessage(r.Context(), interaction.ChannelID, response)
			}
		}()

		// Acknowledge immediately
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"type": 5, // DEFERRED_CHANNEL_MESSAGE_WITH_SOURCE
		})
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Stop signals the bot to shut down.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
}
