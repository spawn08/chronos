---
title: "Chat Interfaces: Discord, Slack & Telegram Bots"
sidebar_label: "Chat Interfaces"
---

Chronos ships three small, self-contained bot adapters — `os/interfaces/discord`,
`os/interfaces/slack`, and `os/interfaces/telegram` — that let you expose a
Chronos [agent](/guides/agents) (or the `Execute`/`Run` entry point of a
[team](/guides/teams)) as a Discord bot, a Slack app, or a Telegram bot. Each
adapter is a plain `Bot` type: you construct it with a platform token and a
`MessageHandler` callback, and the adapter takes care of the platform-specific
HTTP/polling plumbing and turns incoming messages into calls to your handler.

:::note At a glance
- These packages are **not** wired into `chronos serve` or `os/server.go` — you
  run them yourself, either as a standalone `package main` process or mounted
  into your own `http.ServeMux`.
- They are intentionally minimal: each one does real HTTP calls against the
  real Discord/Slack/Telegram APIs (this is not a stub), but none of them do
  request-signature verification, command/manifest registration, retries, or
  rate limiting for you. See the **Production considerations** note in each
  section below before exposing one publicly.
:::

## Discord

`os/interfaces/discord` implements `discord.Bot`, an HTTP handler for
Discord's **Interactions** webhook model (slash commands), not a persistent
Gateway/WebSocket connection.

- `discord.New(token string, handler MessageHandler) *Bot` — `token` is the
  Discord bot token; `handler` is
  `func(ctx context.Context, channelID, userID, content string) (string, error)`.
- `(*Bot).HandleInteraction(w http.ResponseWriter, r *http.Request)` — mount
  this at your **Interactions Endpoint URL**. It answers Discord's `PING`
  (type `1`) with `PONG`, and for application-command interactions (type `2`)
  it acknowledges immediately with a deferred response, runs your handler in
  a goroutine, and posts the result back with `SendMessage`.
- `(*Bot).SendMessage(ctx, channelID, content string) error` — posts a message
  via `POST /channels/{id}/messages` using `Authorization: Bot <token>`.
- `(*Bot).Stop()` — signals shutdown (there is no background goroutine to join
  since interactions are handled per-request).

### Setup

1. Create an application and bot at the [Discord Developer Portal](https://discord.com/developers/applications),
   copy the **Bot Token** into `DISCORD_BOT_TOKEN`.
2. Register your slash command(s) with Discord's REST API yourself (this
   package only receives interactions — it does not register commands).
3. Deploy your process behind a public HTTPS URL and set it as the
   **Interactions Endpoint URL** in the portal, pointing at the path where you
   mounted `bot.HandleInteraction`.

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/os/interfaces/discord"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	a, err := agent.New("support-bot", "Support Bot").
		WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	bot := discord.New(os.Getenv("DISCORD_BOT_TOKEN"),
		func(ctx context.Context, channelID, userID, content string) (string, error) {
			return a.Execute(ctx, content)
		})

	mux := http.NewServeMux()
	mux.HandleFunc("/discord/interactions", bot.HandleInteraction)

	log.Println("discord bot listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

:::caution Production consideration
`HandleInteraction` does **not** verify the `X-Signature-Ed25519` /
`X-Signature-Timestamp` headers Discord sends on every request. Add your own
verification (or a [hook](/guides/hooks)/middleware wrapping the handler)
before exposing this endpoint publicly.
:::

## Slack

`os/interfaces/slack` implements `slack.Bot`, an HTTP handler for the Slack
**Events API**.

- `slack.New(token, signingKey string, handler MessageHandler) *Bot` —
  `token` is the Bot OAuth token (`xoxb-...`); `signingKey` is the app's
  signing secret; `handler` is
  `func(ctx context.Context, channel, user, text, threadTS string) (string, error)`.
- `(*Bot).ServeHTTP` handles the Events API `url_verification` challenge
  automatically, ignores events carrying a `bot_id` (to prevent loops), and
  for `message` events runs your handler in a goroutine, replying in-thread
  via `PostMessage` if the handler returns non-empty text.
- `(*Bot).Start(ctx, addr string) error` — convenience server that mounts
  `ServeHTTP` at **`/slack/events`** and calls `ListenAndServe`.
- `(*Bot).PostMessage(ctx, channel, text, threadTS string) error` — calls
  `chat.postMessage` with `Authorization: Bearer <token>`.
- `(*Bot).Stop() error` — closes the server started by `Start`.

### Setup

1. Create an app at [api.slack.com/apps](https://api.slack.com/apps), add the
   `chat:write` Bot Token Scope, and install it to your workspace to obtain
   the Bot User OAuth Token (`SLACK_BOT_TOKEN`).
2. Under **Event Subscriptions**, enable events and set the Request URL to
   `https://<your-host>/slack/events` — Slack's initial verification
   challenge is answered automatically by `ServeHTTP`.
3. Subscribe to the `message.channels` (or `app_mention`) bot event.
4. Copy the app's **Signing Secret** into `SLACK_SIGNING_SECRET`.

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/os/interfaces/slack"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	a, err := agent.New("support-bot", "Support Bot").
		WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	bot := slack.New(
		os.Getenv("SLACK_BOT_TOKEN"),
		os.Getenv("SLACK_SIGNING_SECRET"),
		func(ctx context.Context, channel, user, text, threadTS string) (string, error) {
			return a.Execute(ctx, text)
		},
	)

	log.Println("slack bot listening on :8080/slack/events")
	log.Fatal(bot.Start(ctx, ":8080"))
}
```

:::caution Production consideration
The `signingKey` passed to `slack.New` is stored on the `Bot` but is
**currently not used** to verify the `X-Slack-Signature` / `X-Slack-Request-Timestamp`
headers in `ServeHTTP`. Requests are accepted without HMAC verification, so
you should add your own verification layer in front of this handler before
exposing it publicly.
:::

## Telegram

`os/interfaces/telegram` implements `telegram.Bot`, which supports **both**
long polling and webhook delivery, plus a helper for inline keyboards.

- `telegram.New(token string, handler MessageHandler) *Bot` — `token` is the
  Bot API token from `@BotFather`; `handler` is
  `func(ctx context.Context, chatID, userID int64, text string) (string, error)`.
- `(*Bot).Start(ctx context.Context) error` — long-polls `getUpdates` in a
  loop (using the stored `offset`), dispatching each text message to your
  handler in a goroutine and replying with `SendMessage`. No public URL is
  needed for this mode.
- `(*Bot).WebhookHandler() http.Handler` — an alternative to `Start`: mount
  this behind a public HTTPS URL and Telegram will push updates to it instead
  of you polling. The package has **no helper for calling Telegram's
  `setWebhook` API** — you register the webhook URL yourself.
- `(*Bot).SendMessage(ctx, chatID int64, text string) error` — calls
  `sendMessage` with `parse_mode: Markdown`.
- `(*Bot).SendInlineKeyboard(ctx, chatID int64, text string, buttons [][]Button) error` —
  sends a message with an inline keyboard, useful for human-in-the-loop
  approval prompts. `Button{Text, CallbackData}`.
- `(*Bot).Stop()` — signals the `Start` polling loop to exit.

### Setup

1. Message [`@BotFather`](https://t.me/BotFather) on Telegram, run `/newbot`,
   and copy the token into `TELEGRAM_BOT_TOKEN`.
2. Choose a delivery mode:
   - **Long polling** (simplest, no public URL required) — call `bot.Start(ctx)`.
   - **Webhook** — mount `bot.WebhookHandler()` on your server, then register
     it with Telegram yourself, e.g.:
     ```bash
     curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/setWebhook" \
       -d "url=https://<your-host>/telegram/webhook"
     ```

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/os/interfaces/telegram"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	a, err := agent.New("support-bot", "Support Bot").
		WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	bot := telegram.New(os.Getenv("TELEGRAM_BOT_TOKEN"),
		func(ctx context.Context, chatID, userID int64, text string) (string, error) {
			return a.Execute(ctx, text)
		})

	log.Println("telegram bot: long-polling for updates")
	log.Fatal(bot.Start(ctx))
}
```

## Wiring in a team instead of a single agent

All three `MessageHandler` types are plain functions, so nothing about them
is agent-specific — swap the closure body for a call into a
[`sdk/team`](/guides/teams) runner (e.g. a sequential or router team's
entry point) if you want the chat surface to front a multi-agent team rather
than a single agent.
