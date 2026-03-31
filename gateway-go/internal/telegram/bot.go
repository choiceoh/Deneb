package telegram

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

)

// UpdateHandler is called for each incoming update.
type UpdateHandler func(ctx context.Context, update *Update)

// Bot manages the Telegram bot lifecycle: long-polling for updates,
// dispatching to handlers, and tracking update offsets.
type Bot struct {
	RunState                     // provides IsRunning(), Stop(), BeginRun(), EndRun()
	client    *Client
	config    *Config
	logger    *slog.Logger
	handlerMu sync.Mutex
	handler   UpdateHandler
	offset    int64
	messages  []*Message // buffered inbound messages for poll RPC
	msgMu     sync.Mutex

	// Update deduplication.
	seen   map[int64]time.Time
	seenMu sync.Mutex
}

// NewBot creates a new bot instance.
func NewBot(client *Client, config *Config, handler UpdateHandler, logger *slog.Logger) *Bot {
	return &Bot{
		client:  client,
		config:  config,
		handler: handler,
		logger:  logger,
		seen:    make(map[int64]time.Time),
	}
}

// Start begins the long-polling loop. Blocks until context is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	pollCtx, ok := b.BeginRun(ctx)
	if !ok {
		return nil
	}
	defer b.EndRun()

	b.logger.Info("telegram bot polling started")
	return b.pollLoop(pollCtx)
}

// SetHandler replaces the update handler. Safe to call while polling.
func (b *Bot) SetHandler(h UpdateHandler) {
	b.handlerMu.Lock()
	defer b.handlerMu.Unlock()
	b.handler = h
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
	backoff := &ExponentialBackoff{
		Initial: 1 * time.Second,
		Max:     30 * time.Second,
		Factor:  1.8,
		Jitter:  0.25,
	}

	for {
		if ctx.Err() != nil {
			b.logger.Info("telegram bot polling stopped")
			return ctx.Err()
		}

		updates, err := b.client.GetUpdates(ctx, b.offset, DefaultPollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.logger.Warn("telegram getUpdates error", "error", err, "backoff", backoff.Current())

			if waitErr := backoff.Wait(ctx); waitErr != nil {
				return waitErr
			}
			continue
		}

		backoff.Reset()

		for i := range updates {
			u := &updates[i]
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}

			// Deduplication.
			if b.isDuplicate(u.UpdateID) {
				continue
			}
			b.markSeen(u.UpdateID)

			// Buffer message for poll RPC. DrainMessages() must be called regularly
			// (typically via RPC poll). If drain falls behind, oldest messages are
			// discarded and a warning is logged.
			if u.Message != nil && CheckAccess(b.config, u.Message).Allowed {
				b.msgMu.Lock()
				b.messages = append(b.messages, u.Message)
				if len(b.messages) > MaxMessageBuffer {
					trimmed := len(b.messages) - MessageBufferTrimTarget
					b.messages = b.messages[trimmed:]
					b.logger.Warn("message buffer trimmed; drain may be too slow",
						"trimmed", trimmed,
						"remaining", MessageBufferTrimTarget,
					)
				}
				b.msgMu.Unlock()
			}

			// Dispatch to handler asynchronously so polling is not blocked
			// by slow inbound processing (YouTube extraction, link enrichment, etc.).
			// Single-user deployment: goroutine explosion is not a concern.
			if b.handler != nil {
				go b.handler(ctx, u)
			}
		}

		b.cleanupSeen()
	}
}

// --- Update deduplication ---

func (b *Bot) isDuplicate(updateID int64) bool {
	b.seenMu.Lock()
	defer b.seenMu.Unlock()
	_, exists := b.seen[updateID]
	return exists
}

func (b *Bot) markSeen(updateID int64) {
	b.seenMu.Lock()
	defer b.seenMu.Unlock()
	b.seen[updateID] = time.Now()
}

func (b *Bot) cleanupSeen() {
	b.seenMu.Lock()
	defer b.seenMu.Unlock()
	if len(b.seen) < MaxDedupeEntries/2 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(DedupeTTLMs) * time.Millisecond)
	for id, ts := range b.seen {
		if ts.Before(cutoff) {
			delete(b.seen, id)
		}
	}
}

// --- Exponential backoff ---

// ExponentialBackoff implements exponential backoff with jitter.
// Matches the TypeScript TELEGRAM_POLL_RESTART_POLICY.
type ExponentialBackoff struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64
	Jitter  float64
	current time.Duration
}

// Current returns the current backoff duration.
func (eb *ExponentialBackoff) Current() time.Duration {
	if eb.current < eb.Initial {
		return eb.Initial
	}
	return eb.current
}

// Wait sleeps for the current backoff duration (with jitter), then increases it.
func (eb *ExponentialBackoff) Wait(ctx context.Context) error {
	d := eb.Current()
	jitterRange := float64(d) * eb.Jitter
	jitter := (rand.Float64()*2 - 1) * jitterRange
	d = time.Duration(float64(d) + jitter)
	if d < 0 {
		d = eb.Initial
	}

	eb.current = time.Duration(float64(eb.Current()) * eb.Factor)
	if eb.current > eb.Max {
		eb.current = eb.Max
	}

	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reset resets the backoff to its initial value.
func (eb *ExponentialBackoff) Reset() {
	eb.current = 0
}
