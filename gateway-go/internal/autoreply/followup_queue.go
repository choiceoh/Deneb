package autoreply

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// FollowupQueueState manages a per-session followup message queue with
// a drain goroutine. Unlike the TS implementation which relies on a single
// event loop, Go requires per-queue synchronization to prevent races
// between Enqueue and the drain goroutine.
type FollowupQueueState struct {
	mu          sync.Mutex
	sessionKey  string
	items       []QueueItem
	draining    bool
	drainCancel context.CancelFunc
	maxItems    int
	debounceMs  int64
	logger      *slog.Logger
}

// FollowupQueueConfig configures a followup queue.
type FollowupQueueConfig struct {
	MaxItems   int
	DebounceMs int64
	Logger     *slog.Logger
}

// DefaultFollowupQueueConfig returns sensible defaults.
func DefaultFollowupQueueConfig() FollowupQueueConfig {
	return FollowupQueueConfig{
		MaxItems:   20,
		DebounceMs: 500,
	}
}

// NewFollowupQueueState creates a new per-session followup queue.
func NewFollowupQueueState(sessionKey string, cfg FollowupQueueConfig) *FollowupQueueState {
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = 20
	}
	if cfg.DebounceMs <= 0 {
		cfg.DebounceMs = 500
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &FollowupQueueState{
		sessionKey: sessionKey,
		maxItems:   cfg.MaxItems,
		debounceMs: cfg.DebounceMs,
		logger:     logger,
	}
}

// DrainFunc is called with the accumulated items when the debounce timer fires.
type DrainFunc func(ctx context.Context, sessionKey string, items []QueueItem)

// Enqueue adds an item to the followup queue. Thread-safe.
// If a drain is not already scheduled, starts a debounced drain goroutine.
func (q *FollowupQueueState) Enqueue(item QueueItem, drainFn DrainFunc) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	item.QueuedAt = time.Now().UnixMilli()

	// Enforce capacity.
	if len(q.items) >= q.maxItems {
		// Drop oldest.
		q.items = q.items[1:]
	}
	q.items = append(q.items, item)

	// Schedule drain if not already running.
	if !q.draining {
		q.draining = true
		ctx, cancel := context.WithCancel(context.Background())
		q.drainCancel = cancel
		go q.debouncedDrain(ctx, drainFn)
	}

	return true
}

// debouncedDrain waits for the debounce period, then drains all accumulated items.
func (q *FollowupQueueState) debouncedDrain(ctx context.Context, drainFn DrainFunc) {
	timer := time.NewTimer(time.Duration(q.debounceMs) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	// Drain under lock.
	q.mu.Lock()
	items := make([]QueueItem, len(q.items))
	copy(items, q.items)
	q.items = q.items[:0]
	q.draining = false
	q.mu.Unlock()

	if len(items) > 0 && drainFn != nil {
		drainFn(ctx, q.sessionKey, items)
	}
}

// Len returns the current queue length. Thread-safe.
func (q *FollowupQueueState) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Cancel stops the pending drain goroutine and clears the queue.
func (q *FollowupQueueState) Cancel() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.drainCancel != nil {
		q.drainCancel()
		q.drainCancel = nil
	}
	q.items = q.items[:0]
	q.draining = false
}

// FollowupQueueRegistry manages per-session followup queues with
// both registry-level and per-queue synchronization.
type FollowupQueueRegistry struct {
	mu     sync.Mutex
	queues map[string]*FollowupQueueState
	config FollowupQueueConfig
}

// NewFollowupQueueRegistry creates a new registry.
func NewFollowupQueueRegistry(cfg FollowupQueueConfig) *FollowupQueueRegistry {
	return &FollowupQueueRegistry{
		queues: make(map[string]*FollowupQueueState),
		config: cfg,
	}
}

// GetOrCreate returns the queue for a session, creating one if needed.
func (r *FollowupQueueRegistry) GetOrCreate(sessionKey string) *FollowupQueueState {
	r.mu.Lock()
	defer r.mu.Unlock()
	q, ok := r.queues[sessionKey]
	if !ok {
		q = NewFollowupQueueState(sessionKey, r.config)
		r.queues[sessionKey] = q
	}
	return q
}

// Remove cancels and removes the queue for a session.
func (r *FollowupQueueRegistry) Remove(sessionKey string) {
	r.mu.Lock()
	q, ok := r.queues[sessionKey]
	if ok {
		delete(r.queues, sessionKey)
	}
	r.mu.Unlock()

	if q != nil {
		q.Cancel()
	}
}

// Count returns the number of active queues.
func (r *FollowupQueueRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queues)
}

// CancelAll cancels and removes all queues.
func (r *FollowupQueueRegistry) CancelAll() {
	r.mu.Lock()
	queues := make(map[string]*FollowupQueueState, len(r.queues))
	for k, v := range r.queues {
		queues[k] = v
	}
	r.queues = make(map[string]*FollowupQueueState)
	r.mu.Unlock()

	for _, q := range queues {
		q.Cancel()
	}
}
