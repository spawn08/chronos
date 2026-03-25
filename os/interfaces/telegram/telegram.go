// Package telegram provides a Telegram bot interface for Chronos agents.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MessageHandler processes incoming messages and returns a response.
type MessageHandler func(ctx context.Context, chatID int64, userID int64, text string) (string, error)

// Bot is a Telegram bot that receives messages via long polling and routes them to an agent.
type Bot struct {
	token   string
	handler MessageHandler
	client  *http.Client
	mu      sync.RWMutex
	stopCh  chan struct{}
	offset  int64
}

// New creates a new Telegram bot.
// token is the Telegram Bot API token from @BotFather.
// handler is called for each incoming message.
func New(token string, handler MessageHandler) *Bot {
	return &Bot{
		token:   token,
		handler: handler,
		client:  &http.Client{Timeout: 35 * time.Second},
		stopCh:  make(chan struct{}),
	}
}

// Start begins long polling for updates.
func (b *Bot) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.stopCh:
			return nil
		default:
			if err := b.pollOnce(ctx); err != nil {
				time.Sleep(time.Second)
			}
		}
	}
}

// Stop signals the bot to stop polling.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
}

func (b *Bot) pollOnce(ctx context.Context) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30",
		b.token, b.offset)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("telegram poll: %w", err)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram poll: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				From struct {
					ID int64 `json:"id"`
				} `json:"from"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("telegram decode: %w", err)
	}

	for _, update := range result.Result {
		b.offset = update.UpdateID + 1
		if update.Message != nil && update.Message.Text != "" {
			go b.handleUpdate(ctx, update.Message.Chat.ID,
				update.Message.From.ID, update.Message.Text)
		}
	}
	return nil
}

func (b *Bot) handleUpdate(ctx context.Context, chatID, userID int64, text string) {
	response, err := b.handler(ctx, chatID, userID, text)
	if err != nil {
		response = fmt.Sprintf("Error: %v", err)
	}
	if response != "" {
		b.SendMessage(ctx, chatID, response)
	}
}

// SendMessage sends a text message to a Telegram chat.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	body := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if !result.OK {
		return fmt.Errorf("telegram send: %s", result.Description)
	}
	return nil
}

// SendInlineKeyboard sends a message with an inline keyboard for HITL confirmations.
func (b *Bot) SendInlineKeyboard(ctx context.Context, chatID int64, text string, buttons [][]Button) error {
	keyboard := make([][]map[string]string, len(buttons))
	for i, row := range buttons {
		keyboard[i] = make([]map[string]string, len(row))
		for j, btn := range row {
			keyboard[i][j] = map[string]string{
				"text":          btn.Text,
				"callback_data": btn.CallbackData,
			}
		}
	}

	body := map[string]any{
		"chat_id":      chatID,
		"text":         text,
		"reply_markup": map[string]any{"inline_keyboard": keyboard},
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("telegram keyboard: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram keyboard: %w", err)
	}
	resp.Body.Close()
	return nil
}

// Button represents an inline keyboard button.
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// WebhookHandler returns an http.Handler for receiving Telegram webhook updates.
func (b *Bot) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var update struct {
			Message *struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				From struct {
					ID int64 `json:"id"`
				} `json:"from"`
				Text string `json:"text"`
			} `json:"message"`
		}
		if err := json.Unmarshal(body, &update); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if update.Message != nil && update.Message.Text != "" {
			go b.handleUpdate(r.Context(), update.Message.Chat.ID,
				update.Message.From.ID, update.Message.Text)
		}

		w.WriteHeader(http.StatusOK)
	})
}
