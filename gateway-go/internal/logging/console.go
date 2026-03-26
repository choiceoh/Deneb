// Package logging provides a human-readable console log handler for slog.
//
// The ConsoleHandler outputs compact, colored log lines in the format:
//
//	HH:MM:SS LVL message key=val key=val
//
// Designed for direct log tailing (tail -f) on a single-server deployment.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"
	"unicode"
)

// ANSI color codes for log level highlighting.
const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorDim    = "\033[2m"
)

// ConsoleOptions configures the ConsoleHandler.
type ConsoleOptions struct {
	// Level is the minimum log level to emit. Defaults to slog.LevelInfo.
	Level slog.Leveler
	// Color enables ANSI color output. Defaults to true.
	Color bool
}

// ConsoleHandler is a slog.Handler that writes human-readable log lines.
type ConsoleHandler struct {
	w        io.Writer
	level    slog.Leveler
	color    bool
	mu       *sync.Mutex
	preAttrs []slog.Attr
	groups   []string
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 256)
		return &b
	},
}

// NewConsoleHandler creates a new ConsoleHandler writing to w.
func NewConsoleHandler(w io.Writer, opts *ConsoleOptions) *ConsoleHandler {
	h := &ConsoleHandler{
		w:     w,
		level: slog.LevelInfo,
		color: true,
		mu:    &sync.Mutex{},
	}
	if opts != nil {
		if opts.Level != nil {
			h.level = opts.Level
		}
		h.color = opts.Color
	}
	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle formats and writes a log record.
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get().(*[]byte)
	buf := (*bp)[:0]

	// Time: HH:MM:SS
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	hour, min, sec := t.Clock()
	buf = appendTwoDigits(buf, hour)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, min)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, sec)
	buf = append(buf, ' ')

	// Level: 3-letter abbreviation with optional color.
	lvl, lvlColor := levelAbbrev(r.Level)
	if h.color {
		buf = append(buf, lvlColor...)
		buf = append(buf, lvl...)
		buf = append(buf, colorReset...)
	} else {
		buf = append(buf, lvl...)
	}
	buf = append(buf, ' ')

	// Message.
	buf = append(buf, r.Message...)

	// Pre-attrs from WithAttrs.
	for _, a := range h.preAttrs {
		buf = h.appendAttr(buf, a)
	}

	// Record attrs.
	r.Attrs(func(a slog.Attr) bool {
		buf = h.appendAttr(buf, a)
		return true
	})

	buf = append(buf, '\n')

	h.mu.Lock()
	_, err := h.w.Write(buf)
	h.mu.Unlock()

	*bp = buf
	bufPool.Put(bp)
	return err
}

// WithAttrs returns a new handler with the given attributes pre-set.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	// Apply group prefix to pre-attrs.
	for _, a := range attrs {
		h2.preAttrs = append(h2.preAttrs, a)
	}
	return h2
}

// WithGroup returns a new handler with the given group name.
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

func (h *ConsoleHandler) clone() *ConsoleHandler {
	h2 := &ConsoleHandler{
		w:     h.w,
		level: h.level,
		color: h.color,
		mu:    h.mu,
	}
	h2.preAttrs = make([]slog.Attr, len(h.preAttrs))
	copy(h2.preAttrs, h.preAttrs)
	h2.groups = make([]string, len(h.groups))
	copy(h2.groups, h.groups)
	return h2
}

// appendAttr formats a single attribute as " key=val".
func (h *ConsoleHandler) appendAttr(buf []byte, a slog.Attr) []byte {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return buf
	}

	buf = append(buf, ' ')

	// Group prefix.
	for _, g := range h.groups {
		buf = append(buf, g...)
		buf = append(buf, '.')
	}

	if h.color {
		buf = append(buf, colorDim...)
		buf = append(buf, a.Key...)
		buf = append(buf, '=')
		buf = append(buf, colorReset...)
	} else {
		buf = append(buf, a.Key...)
		buf = append(buf, '=')
	}

	buf = appendValue(buf, a.Value)
	return buf
}

// appendValue formats an slog.Value, quoting strings that need it.
func appendValue(buf []byte, v slog.Value) []byte {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if needsQuote(s) {
			buf = append(buf, strconv.Quote(s)...)
		} else {
			buf = append(buf, s...)
		}
	case slog.KindTime:
		buf = append(buf, v.Time().Format(time.RFC3339)...)
	case slog.KindDuration:
		buf = append(buf, v.Duration().String()...)
	case slog.KindGroup:
		attrs := v.Group()
		for i, a := range attrs {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = append(buf, a.Key...)
			buf = append(buf, '=')
			buf = appendValue(buf, a.Value.Resolve())
		}
	default:
		buf = append(buf, v.String()...)
	}
	return buf
}

// needsQuote returns true if s should be double-quoted in the output.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if unicode.IsSpace(r) || r == '"' || r == '=' || r == '\\' {
			return true
		}
	}
	return false
}

// levelAbbrev returns the 3-letter abbreviation and ANSI color for a level.
func levelAbbrev(l slog.Level) (string, string) {
	switch {
	case l < slog.LevelInfo:
		return "DBG", colorCyan
	case l < slog.LevelWarn:
		return "INF", colorGreen
	case l < slog.LevelError:
		return "WRN", colorYellow
	default:
		return "ERR", colorRed
	}
}

// appendTwoDigits appends a zero-padded two-digit number to buf.
func appendTwoDigits(buf []byte, n int) []byte {
	buf = append(buf, byte('0'+n/10))
	buf = append(buf, byte('0'+n%10))
	return buf
}
