package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// UpdateHandler is called for each incoming update.
type UpdateHandler func(ctx context.Context, update *Update)

// Bot manages the Telegram bot lifecycle: long-polling for updates,
// dispatching to handlers, and tracking update offsets.
type Bot struct {
	client  *Client
	config  *Config
	handler UpdateHandler
	logger  *slog.Logger

	mu        sync.Mutex
	offset    int64
	running   bool
	stopFunc  context.CancelFunc
	messages  []*Message // buffered inbound messages for poll RPC
	msgMu     sync.Mutex
}

// NewBot creates a new bot instance.
func NewBot(client *Client, config *Config, handler UpdateHandler, logger *slog.Logger) *Bot {
	return &Bot{
		client:  client,
		config:  config,
		handler: handler,
		logger:  logger,
	}
}

// Start begins the long-polling loop. Blocks until context is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return nil
	}
	pollCtx, cancel := context.WithCancel(ctx)
	b.running = true
	b.stopFunc = cancel
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.running = false
		b.stopFunc = nil
		b.mu.Unlock()
	}()

	b.logger.Info("telegram bot polling started")
	return b.pollLoop(pollCtx)
}

// Stop stops the polling loop.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopFunc != nil {
		b.stopFunc()
	}
}

// IsRunning returns whether the bot is currently polling.
func (b *Bot) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// DrainMessages returns and clears the buffered inbound messages.
func (b *Bot) DrainMessages() []*Message {
	b.msgMu.Lock()
	defer b.msgMu.Unlock()
	msgs := b.messages
	b.messages = nil
	return msgs
}

func (b *Bot) pollLoop(ctx context.Context) error {
	pollingTimeout := 30 // seconds, for long polling
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("telegram bot polling stopped")
			return ctx.Err()
		default:
		}

		updates, err := b.getUpdates(ctx, pollingTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.logger.Warn("telegram getUpdates error", "error", err, "backoff", backoff)

			// Exponential backoff on error, max 30s.
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = time.Second // Reset backoff on success.

		for _, update := range updates {
			if update.UpdateID >= b.offset {
				b.offset = update.UpdateID + 1
			}

			// Buffer message for poll RPC.
			if update.Message != nil {
				if b.isAllowed(update.Message) {
					b.msgMu.Lock()
					b.messages = append(b.messages, update.Message)
					// Cap buffer to prevent unbounded growth.
					if len(b.messages) > 1000 {
						b.messages = b.messages[len(b.messages)-500:]
					}
					b.msgMu.Unlock()
				}
			}

			// Dispatch to handler.
			if b.handler != nil {
				b.handler(ctx, &update)
			}
		}
	}
}

func (b *Bot) getUpdates(ctx context.Context, timeout int) ([]Update, error) {
	params := map[string]any{
		"offset":  b.offset,
		"timeout": timeout,
		"allowed_updates": []string{
			"message",
			"edited_message",
			"callback_query",
		},
	}

	result, err := b.client.Call(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}

	var updates []Update
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// isAllowed checks if the message sender is in the allow list.
// If no allow list is configured, all messages are allowed.
func (b *Bot) isAllowed(msg *Message) bool {
	if msg.From == nil {
		return false
	}

	// DM messages: check allowFrom.
	if msg.Chat.Type == "private" {
		if len(b.config.AllowFrom) == 0 {
			return true
		}
		for _, id := range b.config.AllowFrom {
			if id == msg.From.ID {
				return true
			}
		}
		return false
	}

	// Group messages: check groupAllowFrom.
	if len(b.config.GroupAllowFrom) == 0 {
		// Fall back to allowFrom for groups if groupAllowFrom not set.
		if len(b.config.AllowFrom) == 0 {
			return true
		}
		for _, id := range b.config.AllowFrom {
			if id == msg.From.ID {
				return true
			}
		}
		return false
	}
	for _, id := range b.config.GroupAllowFrom {
		if id == msg.From.ID {
			return true
		}
	}
	return false
}
