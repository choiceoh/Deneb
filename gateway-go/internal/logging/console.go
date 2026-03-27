// Package logging provides a human-readable console log handler for slog.
//
// The ConsoleHandler outputs compact, modern log lines with ANSI styling:
//
//	14:05:09.1 INF · [server] request handled status=200 latency=1.2s
//
// Visual hierarchy: dim timestamp, bold colored level, dim · separator,
// dim cyan [pkg] tag, bold message, dim key= with normal values.
// Error attribute values are highlighted in red for quick scanning.
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

// ANSI escape sequences for styling.
const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiDim      = "\033[2m"
	ansiItalic   = "\033[3m"
	ansiRed      = "\033[31m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiBlue     = "\033[34m"
	ansiCyan     = "\033[36m"
	ansiBoldRed  = "\033[1;31m"
	ansiBoldGrn  = "\033[1;32m"
	ansiBoldYel  = "\033[1;33m"
	ansiBoldBlu  = "\033[1;34m"
	ansiBoldCyn  = "\033[1;36m"
	ansiDimCyn   = "\033[2;36m"
)

// pkgAttrKey is the attribute key rendered as a colored [tag] instead of key=value.
const pkgAttrKey = "pkg"

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

	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}

	lvl, lvlStyle := levelLabel(r.Level)
	isErr := r.Level >= slog.LevelError

	// Extract pkg tag from preAttrs and record attrs.
	pkgVal := h.pkgValue()
	if pkgVal == "" {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == pkgAttrKey {
				pkgVal = a.Value.String()
				return false
			}
			return true
		})
	}

	if h.color {
		// Dim timestamp.
		buf = append(buf, ansiDim...)
		buf = appendTimestamp(buf, t)
		buf = append(buf, ansiReset...)
		buf = append(buf, ' ')

		// Bold colored level.
		buf = append(buf, lvlStyle...)
		buf = append(buf, lvl...)
		buf = append(buf, ansiReset...)

		// Dim separator bar.
		buf = append(buf, ' ')
		buf = append(buf, ansiDim...)
		buf = append(buf, "│"...)
		buf = append(buf, ansiReset...)

		// Package tag (if present).
		if pkgVal != "" {
			buf = append(buf, ' ')
			buf = append(buf, ansiDimCyn...)
			buf = append(buf, '[')
			buf = append(buf, pkgVal...)
			buf = append(buf, ']')
			buf = append(buf, ansiReset...)
		}

		buf = append(buf, ' ')

		// Bold message (red if error level).
		if isErr {
			buf = append(buf, ansiBoldRed...)
		} else {
			buf = append(buf, ansiBold...)
		}
		buf = append(buf, r.Message...)
		buf = append(buf, ansiReset...)
	} else {
		buf = appendTimestamp(buf, t)
		buf = append(buf, ' ')
		buf = append(buf, lvl...)
		buf = append(buf, " │ "...)

		// Package tag (if present).
		if pkgVal != "" {
			buf = append(buf, '[')
			buf = append(buf, pkgVal...)
			buf = append(buf, "] "...)
		}

		buf = append(buf, r.Message...)
	}

	// Pre-attrs from WithAttrs (skip pkg, already rendered as tag).
	for _, a := range h.preAttrs {
		if a.Key == pkgAttrKey {
			continue
		}
		buf = h.appendAttr(buf, a, isErr)
	}

	// Record attrs (skip pkg, already rendered as tag).
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == pkgAttrKey {
			return true
		}
		buf = h.appendAttr(buf, a, isErr)
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

// pkgValue returns the value of the "pkg" pre-attribute, if any.
func (h *ConsoleHandler) pkgValue() string {
	for _, a := range h.preAttrs {
		if a.Key == pkgAttrKey {
			return a.Value.String()
		}
	}
	return ""
}

// WithAttrs returns a new handler with the given attributes pre-set.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	h2.preAttrs = append(h2.preAttrs, attrs...)
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

// appendAttr formats a single attribute as " key=val" with styling.
// The "error" key gets special red highlighting for quick scanning.
func (h *ConsoleHandler) appendAttr(buf []byte, a slog.Attr, isErr bool) []byte {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return buf
	}

	buf = append(buf, ' ')

	// Build the full key with group prefix.
	isErrorKey := a.Key == "error" || a.Key == "err" || a.Key == "panic"

	if h.color {
		if isErrorKey {
			// Error keys: italic red key= and red value.
			buf = append(buf, ansiItalic...)
			buf = append(buf, ansiRed...)
			buf = h.appendKey(buf, a.Key)
			buf = append(buf, ansiReset...)
			buf = append(buf, ansiRed...)
			buf = appendValue(buf, a.Value)
			buf = append(buf, ansiReset...)
		} else {
			// Normal keys: dim key=, normal value.
			buf = append(buf, ansiDim...)
			buf = h.appendKey(buf, a.Key)
			buf = append(buf, ansiReset...)
			buf = appendValue(buf, a.Value)
		}
	} else {
		buf = h.appendKey(buf, a.Key)
		buf = appendValue(buf, a.Value)
	}
	return buf
}

// appendKey writes group-prefixed "key=" to buf.
func (h *ConsoleHandler) appendKey(buf []byte, key string) []byte {
	for _, g := range h.groups {
		buf = append(buf, g...)
		buf = append(buf, '.')
	}
	buf = append(buf, key...)
	buf = append(buf, '=')
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
		buf = appendDuration(buf, v.Duration())
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

// appendTimestamp writes "HH:MM:SS.d" (decisecond precision) to buf.
func appendTimestamp(buf []byte, t time.Time) []byte {
	hour, min, sec := t.Clock()
	ds := t.Nanosecond() / 100_000_000 // deciseconds (0-9)
	buf = appendTwoDigits(buf, hour)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, min)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, sec)
	buf = append(buf, '.')
	buf = append(buf, byte('0'+ds))
	return buf
}

// levelLabel returns the 3-letter label and ANSI bold+color for a level.
func levelLabel(l slog.Level) (string, string) {
	switch {
	case l < slog.LevelInfo:
		return "DBG", ansiBoldCyn
	case l < slog.LevelWarn:
		return "INF", ansiBoldBlu
	case l < slog.LevelError:
		return "WRN", ansiBoldYel
	default:
		return "ERR", ansiBoldRed
	}
}

func appendTwoDigits(buf []byte, n int) []byte {
	buf = append(buf, byte('0'+n/10))
	buf = append(buf, byte('0'+n%10))
	return buf
}

// appendDuration writes a human-friendly duration string to buf.
// <1ms → "150µs", <1s → "42ms", <10s → "1.2s", >=10s → Go default "1m30s".
func appendDuration(buf []byte, d time.Duration) []byte {
	switch {
	case d < time.Millisecond:
		buf = strconv.AppendInt(buf, d.Microseconds(), 10)
		buf = append(buf, "µs"...)
	case d < time.Second:
		buf = strconv.AppendInt(buf, d.Milliseconds(), 10)
		buf = append(buf, "ms"...)
	case d < 10*time.Second:
		tenths := d.Milliseconds() / 100
		buf = strconv.AppendFloat(buf, float64(tenths)/10.0, 'f', 1, 64)
		buf = append(buf, 's')
	default:
		buf = append(buf, d.Round(time.Second).String()...)
	}
	return buf
}
