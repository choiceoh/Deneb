package autoreply

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DebounceKeyFunc extracts a grouping key from an inbound item.
// Return "" to bypass debouncing and flush immediately.
type DebounceKeyFunc[T any] func(item T) string

// DebounceFlushFunc processes a batch of debounced items.
type DebounceFlushFunc[T any] func(ctx context.Context, items []T) error

// DebouncerConfig configures the inbound debouncer.
type DebouncerConfig[T any] struct {
	DebounceMs int
	BuildKey   DebounceKeyFunc[T]
	OnFlush    DebounceFlushFunc[T]
	Logger     *slog.Logger
}

type debounceBuffer[T any] struct {
	items      []T
	timer      *time.Timer
	debounceMs int
	retryCount int
}

// Debouncer groups inbound items by key and flushes them after a debounce
// timeout. This mirrors createInboundDebouncer() from the TypeScript codebase.
type Debouncer[T any] struct {
	mu         sync.Mutex
	buffers    map[string]*debounceBuffer[T]
	defaultMs  int
	buildKey   DebounceKeyFunc[T]
	onFlush    DebounceFlushFunc[T]
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
}

const maxDebounceRetries = 3

// NewDebouncer creates a new inbound debouncer.
func NewDebouncer[T any](ctx context.Context, cfg DebouncerConfig[T]) *Debouncer[T] {
	debounceMs := cfg.DebounceMs
	if debounceMs < 0 {
		debounceMs = 0
	}
	dCtx, cancel := context.WithCancel(ctx)
	return &Debouncer[T]{
		buffers:   make(map[string]*debounceBuffer[T]),
		defaultMs: debounceMs,
		buildKey:  cfg.BuildKey,
		onFlush:   cfg.OnFlush,
		logger:    cfg.Logger,
		ctx:       dCtx,
		cancel:    cancel,
	}
}

// Enqueue adds an item to the debounce buffer, flushing immediately if
// debouncing is disabled or the key is empty.
func (d *Debouncer[T]) Enqueue(item T) {
	key := d.buildKey(item)
	canDebounce := d.defaultMs > 0 && key != ""

	if !canDebounce {
		// Flush existing buffer for this key first, then process immediately.
		if key != "" {
			d.FlushKey(key)
		}
		if err := d.onFlush(d.ctx, []T{item}); err != nil && d.logger != nil {
			d.logger.Warn("debounce immediate flush error", "key", key, "error", err)
		}
		return
	}

	d.mu.Lock()
	existing, ok := d.buffers[key]
	if ok {
		existing.items = append(existing.items, item)
		existing.debounceMs = d.defaultMs
		d.scheduleFlushLocked(key, existing)
		d.mu.Unlock()
		return
	}

	buf := &debounceBuffer[T]{
		items:      []T{item},
		debounceMs: d.defaultMs,
	}
	d.buffers[key] = buf
	d.scheduleFlushLocked(key, buf)
	d.mu.Unlock()
}

// FlushKey immediately flushes all buffered items for the given key.
func (d *Debouncer[T]) FlushKey(key string) {
	d.mu.Lock()
	buf, ok := d.buffers[key]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.buffers, key)
	if buf.timer != nil {
		buf.timer.Stop()
		buf.timer = nil
	}
	d.mu.Unlock()

	if len(buf.items) == 0 {
		return
	}
	d.flushBuffer(key, buf)
}

// Close cancels all pending debounce timers.
func (d *Debouncer[T]) Close() {
	d.cancel()
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, buf := range d.buffers {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		delete(d.buffers, key)
	}
}

func (d *Debouncer[T]) scheduleFlushLocked(key string, buf *debounceBuffer[T]) {
	if buf.timer != nil {
		buf.timer.Stop()
	}
	dur := time.Duration(buf.debounceMs) * time.Millisecond
	buf.timer = time.AfterFunc(dur, func() {
		d.mu.Lock()
		current, ok := d.buffers[key]
		if ok && current == buf {
			delete(d.buffers, key)
		}
		d.mu.Unlock()
		if ok && current == buf {
			d.flushBuffer(key, buf)
		}
	})
}

func (d *Debouncer[T]) flushBuffer(key string, buf *debounceBuffer[T]) {
	if len(buf.items) == 0 {
		return
	}
	err := d.onFlush(d.ctx, buf.items)
	if err == nil {
		return
	}
	if d.logger != nil {
		d.logger.Warn("debounce flush error", "key", key, "error", err, "retry", buf.retryCount+1)
	}

	nextRetry := buf.retryCount + 1
	if nextRetry > maxDebounceRetries {
		return
	}

	d.mu.Lock()
	existing, ok := d.buffers[key]
	if ok {
		// Prepend failed items to existing buffer.
		existing.items = append(buf.items, existing.items...)
		if existing.retryCount < nextRetry {
			existing.retryCount = nextRetry
		}
	} else {
		retry := &debounceBuffer[T]{
			items:      buf.items,
			debounceMs: buf.debounceMs,
			retryCount: nextRetry,
		}
		d.buffers[key] = retry
		d.scheduleFlushLocked(key, retry)
	}
	d.mu.Unlock()
}
