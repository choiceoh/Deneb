// Package logging provides a human-readable console log handler for slog.
//
// The ConsoleHandler outputs compact, modern log lines with ANSI styling:
//
//	14:05:09 │ [server] request handled status=200 latency=1.2s
//
// Visual hierarchy: dim timestamp, level-colored │ bar (dim=info, yellow=warn, red=error, cyan=debug),
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
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiItalic  = "\033[3m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiCyan    = "\033[36m"
	ansiBoldRed = "\033[1;31m"
	ansiBoldGrn = "\033[1;32m"
	ansiBoldYel = "\033[1;33m"
	ansiBoldBlu = "\033[1;34m"
	ansiBoldCyn = "\033[1;36m"
	ansiDimCyn  = "\033[2;36m"
)

// pkgAttrKey is the attribute key rendered as a colored [tag] instead of key=value.
const pkgAttrKey = "pkg"

// ConsoleOptions configures the ConsoleHandler.
type ConsoleOptions struct {
	// Level is the minimum log level to emit. Defaults to slog.LevelInfo.
	Level slog.Leveler
	// Color enables ANSI color output. Defaults to true.
	Color bool
	// ReplaceAttr is called for every attribute before it is rendered, with
	// the same semantics as slog.HandlerOptions.ReplaceAttr (see the stdlib
	// doc for details):
	//   - groups is the list of currently open group keys (empty for top-level).
	//   - The Attr's Value is resolved before the callback runs.
	//   - Returning a zero Attr (slog.Attr{}) drops the attribute.
	//   - Group-kind Attrs are NOT passed; only their contents are.
	// Nil means no replacement (pre-change behavior, byte-identical output).
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

// ConsoleHandler is a slog.Handler that writes human-readable log lines.
type ConsoleHandler struct {
	w           io.Writer
	level       slog.Leveler
	color       bool
	mu          *sync.Mutex
	preAttrs    []slog.Attr
	groups      []string
	replaceAttr func(groups []string, a slog.Attr) slog.Attr
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
		h.replaceAttr = opts.ReplaceAttr
	}
	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle formats and writes a log record.
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get().(*[]byte) //nolint:errcheck // type is guaranteed by pool
	buf := (*bp)[:0]

	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}

	barStyle := levelBarStyle(r.Level)
	isErr := r.Level >= slog.LevelError

	// Extract pkg tag from preAttrs and record attrs. The pkg tag is a visual
	// hint (module/package name), not a secret-bearing attribute, so it is
	// intentionally rendered before the ReplaceAttr pipeline — callers who
	// inject `pkg` are trusted infrastructure code, never user input.
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

		// Level-colored separator bar (replaces text label).
		buf = append(buf, ' ')
		buf = append(buf, barStyle...)
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
		buf = append(buf, levelText(r.Level)...)
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
		w:           h.w,
		level:       h.level,
		color:       h.color,
		mu:          h.mu,
		replaceAttr: h.replaceAttr,
	}
	h2.preAttrs = make([]slog.Attr, len(h.preAttrs))
	copy(h2.preAttrs, h.preAttrs)
	h2.groups = make([]string, len(h.groups))
	copy(h2.groups, h.groups)
	return h2
}

// continuationIndent is the visual width of "HH:MM:SS │ " used to align
// continuation lines (attrs whose key starts with "\n").
const continuationIndent = "           " // 11 spaces: 8 (time) + 1 + 1 (│) + 1

// appendAttr formats a single attribute as " key=val" with styling.
// If the key starts with "\n", the attr is rendered on a new indented line
// (the "\n" prefix is stripped from the displayed key).
// The "error" key gets special red highlighting for quick scanning.
//
// When the handler's ReplaceAttr is non-nil the attribute is passed through
// it first (with resolved value, matching slog.JSONHandler semantics): a zero
// return drops the attr, a modified return replaces it. Group-kind attrs are
// not passed to ReplaceAttr; their contents are handled by appendGroupValue.
func (h *ConsoleHandler) appendAttr(buf []byte, a slog.Attr, _ bool) []byte {
	a.Value = a.Value.Resolve()
	// ReplaceAttr hook — mirror slog.commonHandler.appendAttr: skip for
	// KindGroup (the group's contents will be walked by appendGroupValue with
	// per-content replacement applied).
	if h.replaceAttr != nil && a.Value.Kind() != slog.KindGroup {
		a = h.replaceAttr(h.groups, a)
		a.Value = a.Value.Resolve()
	}
	if a.Equal(slog.Attr{}) {
		return buf
	}

	// Continuation-line attrs: start a new line aligned past the timestamp+bar.
	if a.Key != "" && a.Key[0] == '\n' {
		a.Key = a.Key[1:]
		buf = append(buf, '\n')
		buf = append(buf, continuationIndent...)
	} else {
		buf = append(buf, ' ')
	}

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
			buf = h.appendValue(buf, a.Value, a.Key)
			buf = append(buf, ansiReset...)
		} else {
			// Normal keys: dim key=, normal value.
			buf = append(buf, ansiDim...)
			buf = h.appendKey(buf, a.Key)
			buf = append(buf, ansiReset...)
			buf = h.appendValue(buf, a.Value, a.Key)
		}
	} else {
		buf = h.appendKey(buf, a.Key)
		buf = h.appendValue(buf, a.Value, a.Key)
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
//
// groupKey is the owning attribute's key (non-empty for a named Group value).
// When the value is a KindGroup, the key is pushed onto the groups stack
// that ReplaceAttr sees while the group's contents are walked — matching
// slog.JSONHandler's behavior for nested groups.
func (h *ConsoleHandler) appendValue(buf []byte, v slog.Value, groupKey string) []byte {
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
		buf = h.appendGroupValue(buf, v.Group(), groupKey)
	default:
		buf = append(buf, v.String()...)
	}
	return buf
}

// appendGroupValue renders a KindGroup's contents inline as "k=v k=v", pushing
// groupKey onto the replaceAttr groups stack (if non-empty) so any ReplaceAttr
// hook sees the correct ancestry for each nested attribute.
func (h *ConsoleHandler) appendGroupValue(buf []byte, attrs []slog.Attr, groupKey string) []byte {
	// Build the groups slice visible to the replacer for this recursion only.
	// Copy to avoid mutating the handler's shared slice.
	var nested []string
	if h.replaceAttr != nil {
		if groupKey != "" {
			nested = make([]string, 0, len(h.groups)+1)
			nested = append(nested, h.groups...)
			nested = append(nested, groupKey)
		} else {
			nested = h.groups
		}
	}

	first := true
	for _, a := range attrs {
		a.Value = a.Value.Resolve()
		if h.replaceAttr != nil && a.Value.Kind() != slog.KindGroup {
			a = h.replaceAttr(nested, a)
			a.Value = a.Value.Resolve()
		}
		if a.Equal(slog.Attr{}) {
			continue
		}
		if !first {
			buf = append(buf, ' ')
		}
		first = false
		buf = append(buf, a.Key...)
		buf = append(buf, '=')
		// Recurse with a handler view that has the group stack advanced so
		// deeper groups render correctly.
		if a.Value.Kind() == slog.KindGroup {
			h2 := h.withGroupsFrame(nested)
			buf = h2.appendValue(buf, a.Value, a.Key)
		} else {
			buf = h.appendValue(buf, a.Value, a.Key)
		}
	}
	return buf
}

// withGroupsFrame returns a shallow view of h with a different groups slice
// for the duration of a nested group render. It is not a full clone — only
// the groups field differs, and the returned value is not stored.
func (h *ConsoleHandler) withGroupsFrame(groups []string) *ConsoleHandler {
	h2 := *h
	h2.groups = groups
	return &h2
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

// appendTimestamp writes "HH:MM:SS" to buf.
func appendTimestamp(buf []byte, t time.Time) []byte {
	hour, minute, sec := t.Clock()
	buf = appendTwoDigits(buf, hour)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, minute)
	buf = append(buf, ':')
	buf = appendTwoDigits(buf, sec)
	return buf
}

// levelText returns a short label used in no-color mode (color mode uses bar style instead).
func levelText(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DBG"
	case l < slog.LevelWarn:
		return "INF"
	case l < slog.LevelError:
		return "WRN"
	default:
		return "ERR"
	}
}

// levelBarStyle returns the ANSI style for the │ bar based on log level.
func levelBarStyle(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return ansiBoldCyn
	case l < slog.LevelWarn:
		return ansiDim
	case l < slog.LevelError:
		return ansiBoldYel
	default:
		return ansiBoldRed
	}
}

func appendTwoDigits(buf []byte, n int) []byte {
	buf = append(buf, byte('0'+n/10)) //nolint:gosec // G115 — n is 0-59 (time digit), safe
	buf = append(buf, byte('0'+n%10)) //nolint:gosec // G115 — n is 0-59 (time digit), safe
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
