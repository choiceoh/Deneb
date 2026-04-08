// reply_dispatcher_full.go — Full reply dispatcher with human delay and idle signaling.
// Mirrors src/auto-reply/reply/reply-dispatcher.ts (264 LOC).
package streaming

import (
	"context"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"log/slog"
	"sync"
	"time"
)

// HumanDelayConfig configures human-like delays between reply chunks.
type HumanDelayConfig struct {
	MinMs int64 // minimum delay between messages
	MaxMs int64 // maximum delay
}

// DefaultHumanDelay returns sensible human-like delay defaults.
func DefaultHumanDelay() HumanDelayConfig {
	return HumanDelayConfig{MinMs: 500, MaxMs: 2000}
}

// FullReplyDispatcher extends ReplyDispatcher with human delay, timeout
// wrapping, idle signaling, and pending count reservation.
type FullReplyDispatcher struct {
	mu          sync.Mutex
	deliver     types.DeliverFunc
	logger      *slog.Logger
	counts      map[types.ReplyDispatchKind]int
	pending     int
	complete    bool
	idleCh      chan struct{}
	humanDelay  HumanDelayConfig
	sendTimeout time.Duration
	onComplete  func()
	lastSendAt  int64
}

// FullDispatcherConfig configures the full reply dispatcher.
type FullDispatcherConfig struct {
	Deliver     types.DeliverFunc
	Logger      *slog.Logger
	HumanDelay  HumanDelayConfig
	SendTimeout time.Duration
	OnComplete  func() // called when all sends are done
}

// NewFullReplyDispatcher creates a new full dispatcher.
func NewFullReplyDispatcher(cfg FullDispatcherConfig) *FullReplyDispatcher {
	sendTimeout := cfg.SendTimeout
	if sendTimeout <= 0 {
		sendTimeout = 15 * time.Second
	}
	return &FullReplyDispatcher{
		deliver:     cfg.Deliver,
		logger:      cfg.Logger,
		counts:      make(map[types.ReplyDispatchKind]int),
		idleCh:      make(chan struct{}),
		humanDelay:  cfg.HumanDelay,
		sendTimeout: sendTimeout,
		onComplete:  cfg.OnComplete,
	}
}

// SendTool delivers a tool result payload.
func (d *FullReplyDispatcher) SendTool(ctx context.Context, payload types.ReplyPayload) bool {
	return d.send(ctx, payload, types.DispatchKindTool)
}

// SendBlock delivers a block reply payload (streaming chunk).
func (d *FullReplyDispatcher) SendBlock(ctx context.Context, payload types.ReplyPayload) bool {
	return d.send(ctx, payload, types.DispatchKindBlock)
}

// SendFinal delivers the final reply payload.
func (d *FullReplyDispatcher) SendFinal(ctx context.Context, payload types.ReplyPayload) bool {
	return d.send(ctx, payload, types.DispatchKindFinal)
}

func (d *FullReplyDispatcher) send(ctx context.Context, payload types.ReplyPayload, kind types.ReplyDispatchKind) bool {
	d.mu.Lock()
	if d.complete {
		d.mu.Unlock()
		return false
	}
	d.pending++
	d.counts[kind]++
	d.mu.Unlock()

	// Apply human-like delay between sends.
	d.applyHumanDelay(ctx)

	// Wrap delivery with timeout.
	deliverCtx, cancel := context.WithTimeout(ctx, d.sendTimeout)
	defer cancel()

	if err := d.deliver(deliverCtx, payload, kind); err != nil {
		d.logger.Warn("reply dispatch error", "kind", kind, "error", err)
	}

	d.mu.Lock()
	d.pending--
	d.lastSendAt = time.Now().UnixMilli()
	idle := d.pending == 0
	d.mu.Unlock()

	if idle {
		select {
		case d.idleCh <- struct{}{}:
		default:
		}
	}
	return true
}

func (d *FullReplyDispatcher) applyHumanDelay(ctx context.Context) {
	d.mu.Lock()
	last := d.lastSendAt
	minDelay := d.humanDelay.MinMs
	d.mu.Unlock()

	if minDelay <= 0 || last == 0 {
		return
	}
	elapsed := time.Now().UnixMilli() - last
	if elapsed < minDelay {
		delay := minDelay - elapsed
		if maxDelay := d.humanDelay.MaxMs; maxDelay > 0 && delay > maxDelay {
			delay = maxDelay
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(delay) * time.Millisecond):
		}
	}
}

// MarkComplete prevents further sends and fires the onComplete callback.
func (d *FullReplyDispatcher) MarkComplete() {
	d.mu.Lock()
	if d.complete {
		d.mu.Unlock()
		return
	}
	d.complete = true
	cb := d.onComplete
	d.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// WaitForIdle blocks until all pending sends complete.
func (d *FullReplyDispatcher) WaitForIdle(ctx context.Context, timeout time.Duration) bool {
	d.mu.Lock()
	if d.pending == 0 {
		d.mu.Unlock()
		return true
	}
	d.mu.Unlock()

	select {
	case <-d.idleCh:
		return true
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
}

// Counts returns the number of sends per dispatch kind.
func (d *FullReplyDispatcher) Counts() map[types.ReplyDispatchKind]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make(map[types.ReplyDispatchKind]int)
	for k, v := range d.counts {
		result[k] = v
	}
	return result
}

// PendingCount returns the number of in-flight sends.
func (d *FullReplyDispatcher) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pending
}

// IsComplete returns true if the dispatcher has been marked complete.
func (d *FullReplyDispatcher) IsComplete() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.complete
}
