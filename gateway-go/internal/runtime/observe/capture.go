// Package observe is Deneb's unified observation plane — the single access
// point a coding agent (external Claude Code, the internal self-evolution loop,
// or a human operator) uses to watch the gateway's logging, runtime behavior,
// and the code paths that produced them.
//
// # Why this package exists
//
// Before observe, the signals a coding agent needs to improve Deneb were
// scattered across six stores in three formats:
//
//   - logging         → stderr → journald (prod) / shell-redirect (dev), slog text/json
//   - turn behavior   → ~/.deneb/agent-logs/{session}.jsonl  (agentlog, #2108)
//   - cache/RPC metrics → in-memory, lost on restart
//   - cron runs       → ~/.deneb/cron/runs/*.jsonl  (separate scheme)
//   - quality scores  → ~/.deneb/quality-results.db  (sqlite)
//   - transcripts     → ~/.deneb/transcripts/*.jsonl
//
// A run carries a stable runId (chat/run.go tags every log line with it), yet
// nothing joined the log lines, the agentlog turn-shape, and the transcript for
// one run into a single view. And the external entry point that used to expose
// any of this (the standalone MCP server) was deleted.
//
// # What observe adds
//
// observe is turn-centric: runId is the first-class key. It contributes the one
// missing collector — an in-memory ring buffer that captures every slog Record
// with its runId/session tags (LogCapture, a slog.Handler) — and joins it with
// the agentlog turn-shape that already exists. The handler/observe RPC package
// exposes the join as observe.turn / observe.logs / observe.behavior /
// observe.health, all on one JSON schema. Three thin adapters (external CLI,
// internal chat tool, native dashboard) sit on top of that one core.
//
// The ring buffer deliberately does not persist: journald already holds the
// durable log, and an agent debugging "what just happened on run X" wants the
// recent window, not history. Long-lived behavioral history lives in agentlog.
package observe

import (
	"context"
	"log/slog"
	"sync"
)

// DefaultRingSize is the number of recent log lines LogCapture retains. ~5k
// lines at a few hundred bytes each is ~1-2 MB resident — negligible even on
// the unified-memory DGX host, and enough to cover several agent turns' worth
// of log output for after-the-fact inspection.
const DefaultRingSize = 5000

// LogLine is one captured slog record, flattened for JSON transport. It pulls
// runId and session out of the attribute set into top-level fields because they
// are the join keys observe is built around; everything else stays in Attrs.
type LogLine struct {
	Ts      int64             `json:"ts"` //nolint:staticcheck // ST1003 — wire field name (unix millis)
	Level   string            `json:"level"`
	Msg     string            `json:"msg"`
	RunID   string            `json:"runId,omitempty"`
	Session string            `json:"session,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`

	// lvl is the numeric level kept for cheap MinLevel comparison at query
	// time; never serialized (the string Level is the wire form).
	lvl slog.Level `json:"-"`
}

// QueryOpts filters a ring scan. Zero-value fields are "don't filter".
type QueryOpts struct {
	RunID    string     // exact runId match
	Session  string     // exact session match
	MinLevel slog.Level // records below this level are skipped (zero = Info)
	SinceMs  int64      // only records with Ts >= SinceMs
	Contains string     // case-sensitive substring match on Msg
	Limit    int        // max lines (default 200); newest-first
}

const defaultQueryLimit = 200

// ParseLevel maps a level string ("debug"/"info"/"warn"/"error") to slog.Level,
// defaulting to Debug for an empty/unknown value so a query with no level filter
// returns everything. (Note this differs from bootstrap.ParseLogLevel, whose
// empty default is Info — there it gates output; here it gates a read filter.)
func ParseLevel(s string) slog.Level {
	switch s {
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// Ring is a fixed-size circular buffer of LogLines, safe for concurrent
// append (from every goroutine that logs) and query (from RPC handlers).
type Ring struct {
	mu   sync.Mutex
	buf  []LogLine
	size int
	next int  // index the next append writes to
	full bool // whether the buffer has wrapped at least once
}

// NewRing creates a ring holding the most recent size lines. A size <= 0
// falls back to DefaultRingSize.
func NewRing(size int) *Ring {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &Ring{buf: make([]LogLine, size), size: size}
}

func (r *Ring) append(l LogLine) {
	r.mu.Lock()
	r.buf[r.next] = l
	r.next = (r.next + 1) % r.size
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Query returns matching lines newest-first, capped at opts.Limit.
func (r *Ring) Query(opts QueryOpts) []LogLine {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Number of populated slots, and the index of the newest entry.
	count := r.size
	if !r.full {
		count = r.next
	}

	out := make([]LogLine, 0, min(limit, count))
	for i := range count {
		// Walk backwards from the newest entry. +r.size keeps the modulo
		// non-negative without a branch.
		idx := (r.next - 1 - i + r.size) % r.size
		l := r.buf[idx]
		if !matchLine(l, opts) {
			continue
		}
		out = append(out, l)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Cap is the ring's fixed capacity.
func (r *Ring) Cap() int { return r.size }

// Len is how many lines are currently retained (≤ Cap).
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return r.size
	}
	return r.next
}

func matchLine(l LogLine, o QueryOpts) bool {
	if o.RunID != "" && l.RunID != o.RunID {
		return false
	}
	if o.Session != "" && l.Session != o.Session {
		return false
	}
	if l.lvl < o.MinLevel {
		return false
	}
	if o.SinceMs > 0 && l.Ts < o.SinceMs {
		return false
	}
	if o.Contains != "" && !containsSub(l.Msg, o.Contains) {
		return false
	}
	return true
}

// containsSub is strings.Contains, inlined to keep the hot path import-free.
func containsSub(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// LogCapture is a slog.Handler that records every passing log line into a Ring
// and then forwards to its delegate unchanged. It sits inside the server's
// swappable handler chain (see server.go) so it observes ALL records — Info and
// up — regardless of later wraps like notifySlogHandler.
//
// Because slog applies With() attributes by wrapping handlers rather than
// mutating the record, LogCapture accumulates its own attrs across WithAttrs so
// a run-scoped logger (logger.With("session", …, "runId", …)) still surfaces
// those join keys at Handle time.
type LogCapture struct {
	ring     *Ring
	delegate slog.Handler
	attrs    []slog.Attr // accumulated via WithAttrs (outer→inner order)
}

// NewCapture wraps delegate so every handled record is also appended to ring.
// Returns the delegate unchanged when it is nil (nothing to wrap).
func NewCapture(delegate slog.Handler, ring *Ring) *LogCapture {
	return &LogCapture{ring: ring, delegate: delegate}
}

// Ring exposes the underlying buffer so RPC handlers can query it.
func (c *LogCapture) Ring() *Ring { return c.ring }

func (c *LogCapture) Enabled(ctx context.Context, level slog.Level) bool {
	return c.delegate.Enabled(ctx, level)
}

func (c *LogCapture) Handle(ctx context.Context, r slog.Record) error {
	if c.ring != nil {
		c.ring.append(c.toLine(r))
	}
	return c.delegate.Handle(ctx, r)
}

func (c *LogCapture) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return c
	}
	merged := make([]slog.Attr, 0, len(c.attrs)+len(attrs))
	merged = append(merged, c.attrs...)
	merged = append(merged, attrs...)
	return &LogCapture{ring: c.ring, delegate: c.delegate.WithAttrs(attrs), attrs: merged}
}

func (c *LogCapture) WithGroup(name string) slog.Handler {
	// Groups namespace nested attrs; Deneb's logging is essentially flat, so we
	// don't fold the group prefix into captured keys — we only keep the delegate
	// chain correct for output formatting.
	if name == "" {
		return c
	}
	return &LogCapture{ring: c.ring, delegate: c.delegate.WithGroup(name), attrs: c.attrs}
}

// toLine flattens a record plus the handler's accumulated With() attrs into a
// LogLine, hoisting runId/session into dedicated fields.
func (c *LogCapture) toLine(r slog.Record) LogLine {
	line := LogLine{
		Ts:    r.Time.UnixMilli(),
		Level: r.Level.String(),
		Msg:   r.Message,
		lvl:   r.Level,
	}
	var attrs map[string]string
	consume := func(a slog.Attr) {
		switch a.Key {
		case "runId":
			line.RunID = a.Value.String()
		case "session", "sessionKey":
			if line.Session == "" {
				line.Session = a.Value.String()
			}
		default:
			if attrs == nil {
				attrs = make(map[string]string, 4)
			}
			attrs[a.Key] = a.Value.String()
		}
	}
	for _, a := range c.attrs {
		consume(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		consume(a)
		return true
	})
	line.Attrs = attrs
	return line
}
