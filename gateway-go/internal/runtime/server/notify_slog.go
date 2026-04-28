// notify_slog.go — slog.Handler that forwards selected log records to the
// secondary monitoring chat.
//
// The 4 broadcast events covered by the broadcast tap don't catch every
// user-impacting failure. Many failure paths emit slog.Error directly:
// panic recoveries, tool execution errors, LLM provider 5xx, embedding
// service down, persistence failures. Surfacing them to the monitoring
// chat lets the operator notice without grepping logs.
//
// Anti-loop guards (critical):
//   - The notifier's own send failures (`notify send failed`) are
//     suppressed by message prefix to prevent storm-on-failure loops.
//   - The forwarder runs on a separate buffered channel; if the channel
//     fills, records are dropped silently (never a log emit on overflow).
//   - Per-message rate limit: only one mirror per 30s for the same
//     short-key prefix, sharing the same debounce machinery as the
//     broadcast tap so coordinated failure modes are coalesced.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// swappableHandler is a slog.Handler that delegates to a swappable inner
// handler. Used so server.logger can be captured by subsystems early in
// startup (before the notify service exists) and later transparently
// upgraded to a wrapping handler that mirrors ERROR records to the
// monitoring chat. The swap is atomic so concurrent log calls see one
// or the other handler — never a torn state.
type swappableHandler struct {
	mu    sync.RWMutex
	inner slog.Handler
}

// newSwappableHandler wraps the given delegate as the initial inner
// handler. Returns nil if delegate is nil so the caller can skip wiring.
func newSwappableHandler(delegate slog.Handler) *swappableHandler {
	if delegate == nil {
		return nil
	}
	return &swappableHandler{inner: delegate}
}

// currentInner returns the current inner handler. Used by the wrapper
// installer so the new wrap delegates to the original handler rather
// than recursively wrapping itself.
func (s *swappableHandler) currentInner() slog.Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner
}

// Swap replaces the inner handler. Accepts nil as a no-op (treated as
// "leave previous handler installed") so callers don't need to nil-check
// when computing the replacement conditionally.
func (s *swappableHandler) Swap(h slog.Handler) {
	if h == nil {
		return
	}
	s.mu.Lock()
	s.inner = h
	s.mu.Unlock()
}

func (s *swappableHandler) Handle(ctx context.Context, r slog.Record) error {
	s.mu.RLock()
	h := s.inner
	s.mu.RUnlock()
	return h.Handle(ctx, r)
}

func (s *swappableHandler) Enabled(ctx context.Context, level slog.Level) bool {
	s.mu.RLock()
	h := s.inner
	s.mu.RUnlock()
	return h.Enabled(ctx, level)
}

func (s *swappableHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Swappable wrapping does NOT propagate attrs to the inner handler's
	// future swap. Subsystems that call With() before the swap capture a
	// chained handler that won't see the wrap. Acceptable: the gateway
	// rarely chains With() at startup, and ERROR records still reach the
	// inner via the unchained path used by most call sites.
	s.mu.RLock()
	h := s.inner
	s.mu.RUnlock()
	return h.WithAttrs(attrs)
}

func (s *swappableHandler) WithGroup(name string) slog.Handler {
	s.mu.RLock()
	h := s.inner
	s.mu.RUnlock()
	return h.WithGroup(name)
}

// slogForwardMinLevel gates which records are forwarded. ERROR-and-above
// only by default; WARN is too noisy for a summary-grade chat.
var slogForwardMinLevel = slog.LevelError

// notifySlogHandler is a slog.Handler that wraps a delegate (the existing
// gateway logger) and additionally forwards ERROR records to the notify
// service's queue.
type notifySlogHandler struct {
	delegate slog.Handler
	notify   *notifyService
	// Suppress patterns: log messages matching any of these prefixes are
	// forwarded to the delegate but NOT mirrored. Prevents the notifier's
	// own failure logs from triggering more mirrors (loop prevention).
	suppressPrefixes []string
}

// newNotifySlogHandler wraps delegate so ERROR records are mirrored to n.
// When n is nil the delegate is returned unchanged — zero overhead path
// for deployments without a monitoring chat.
func newNotifySlogHandler(delegate slog.Handler, n *notifyService) slog.Handler {
	if n == nil || delegate == nil {
		return delegate
	}
	return &notifySlogHandler{
		delegate: delegate,
		notify:   n,
		suppressPrefixes: []string{
			"notify send failed",     // self-loop on monitoring chat outage
			"notify queue full",      // tap drop log
			"panic in broadcast tap", // dispatchTaps recovered panic
			"panic in notify worker", // worker recovered panic
			"panic in notify slog forwarder",
		},
	}
}

func (h *notifySlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.delegate.Enabled(ctx, level)
}

func (h *notifySlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &notifySlogHandler{
		delegate:         h.delegate.WithAttrs(attrs),
		notify:           h.notify,
		suppressPrefixes: h.suppressPrefixes,
	}
}

func (h *notifySlogHandler) WithGroup(name string) slog.Handler {
	return &notifySlogHandler{
		delegate:         h.delegate.WithGroup(name),
		notify:           h.notify,
		suppressPrefixes: h.suppressPrefixes,
	}
}

// Handle forwards the record to the delegate first (the operator's primary
// log surface) then optionally enqueues it for the monitoring chat. The
// delegate must succeed regardless of mirror state.
func (h *notifySlogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always run the delegate first — never lose a log line because of
	// monitoring failures.
	delegateErr := h.delegate.Handle(ctx, r)

	if r.Level < slogForwardMinLevel {
		return delegateErr
	}
	if h.shouldSuppress(r.Message) {
		return delegateErr
	}

	body := formatSlogRecord(r)
	if body == "" {
		return delegateErr
	}
	h.notify.enqueueLog(body, r.Message)
	return delegateErr
}

func (h *notifySlogHandler) shouldSuppress(msg string) bool {
	for _, pfx := range h.suppressPrefixes {
		if strings.HasPrefix(msg, pfx) {
			return true
		}
	}
	return false
}

// formatSlogRecord renders a record as a Korean alert line plus key=value
// attributes (truncated). Keeps message and the 3 most useful fields:
// error, session, channel.
func formatSlogRecord(r slog.Record) string {
	var b strings.Builder
	switch {
	case r.Level >= slog.LevelError:
		b.WriteString("🔴 ")
	case r.Level >= slog.LevelWarn:
		b.WriteString("🟡 ")
	default:
		b.WriteString("ℹ️ ")
	}
	b.WriteString(r.Message)

	// Surface the most-relevant context fields.
	var errVal, sessionVal, channelVal string
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "error":
			errVal = a.Value.String()
		case "session", "sessionKey":
			sessionVal = a.Value.String()
		case "channel":
			channelVal = a.Value.String()
		}
		return true
	})
	if sessionVal != "" {
		fmt.Fprintf(&b, "\n세션: %s", sessionVal)
	}
	if channelVal != "" {
		fmt.Fprintf(&b, "\n채널: %s", channelVal)
	}
	if errVal != "" {
		fmt.Fprintf(&b, "\n에러: %s", truncate(errVal, 200))
	}
	return b.String()
}

// enqueueLog is the notifyService entry point for forwarded log records.
// Uses the same debounce machinery (keyed by message prefix) as broadcast
// taps so a flapping panic doesn't spam the chat.
func (n *notifyService) enqueueLog(body, message string) {
	if n == nil || body == "" {
		return
	}
	// Debounce key derives from a short prefix of the message so distinct
	// errors aren't lumped together but a tight loop on the same panic is.
	key := "log:" + truncateASCII(message, 60)
	if !n.checkDebounce(key) {
		return
	}
	select {
	case n.queue <- notifyEvent{name: "_slog", payload: body}:
		n.markSent(key)
	default:
		// Silent drop on overflow: a log emit here would be a meta-loop.
	}
}

// truncateASCII clamps to maxBytes by byte count (assumes ASCII-only
// input — log messages are written by us in English). Cheaper than the
// rune-aware truncate used for user-facing Korean text.
func truncateASCII(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}
