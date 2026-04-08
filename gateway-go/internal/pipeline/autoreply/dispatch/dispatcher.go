package dispatch

import (
	"context"
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// ReplyDispatcher manages serialized delivery of tool results, block replies,
// and final replies. This mirrors the TS ReplyDispatcher.
type ReplyDispatcher struct {
	mu       sync.Mutex
	deliver  types.DeliverFunc
	logger   *slog.Logger
	counts   map[types.ReplyDispatchKind]int
	complete bool
}

// NewReplyDispatcher creates a new dispatcher.
func NewReplyDispatcher(deliver types.DeliverFunc, logger *slog.Logger) *ReplyDispatcher {
	return &ReplyDispatcher{
		deliver: deliver,
		logger:  logger,
		counts:  make(map[types.ReplyDispatchKind]int),
	}
}

// Send delivers a reply payload with the given dispatch kind.
// Returns false if the dispatcher has been marked complete.
func (d *ReplyDispatcher) Send(ctx context.Context, payload types.ReplyPayload, kind types.ReplyDispatchKind) bool {
	d.mu.Lock()
	if d.complete {
		d.mu.Unlock()
		return false
	}
	d.counts[kind]++
	d.mu.Unlock()

	if err := d.deliver(ctx, payload, kind); err != nil {
		d.logger.Warn("reply dispatch error", "kind", kind, "error", err)
	}
	return true
}

// MarkComplete prevents further sends.
func (d *ReplyDispatcher) MarkComplete() {
	d.mu.Lock()
	d.complete = true
	d.mu.Unlock()
}

// Counts returns the number of sends per dispatch kind.
func (d *ReplyDispatcher) Counts() map[types.ReplyDispatchKind]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make(map[types.ReplyDispatchKind]int)
	for k, v := range d.counts {
		result[k] = v
	}
	return result
}
