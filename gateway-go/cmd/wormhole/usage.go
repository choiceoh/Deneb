package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Usage metering — wormhole is the single gate every model consumer passes
// through, which makes it the natural single source of truth for "how many
// tokens/requests did each model actually burn this month" (the ClawRouter
// /v1/usage pattern). Today that question is answered by grepping agent logs;
// this meter answers it with one GET.
//
// Design constraints, in order:
//  1. Byte transparency (APC 불가침): metering NEVER touches the request and
//     NEVER mutates the response stream — it reads token counts from a bounded
//     tail copy of the bytes already flowing to the client. In particular it
//     does NOT inject stream_options.include_usage; a stream that carries no
//     usage frame simply meters zero tokens (requests still count).
//  2. Restart-proof: counters persist to usage.json next to the config file
//     (atomic replace), keyed by UTC month window, so `Restart=on-failure`
//     respawns don't zero the month.
//  3. Best-effort: parse failures and persist failures never fail a request.

// usageTailBytes bounds how much response tail is retained for usage parsing.
// The usage object rides in the final SSE frame / at the end of a JSON body,
// so a bounded tail is enough — and a multi-GB stream never buffers.
const usageTailBytes = 64 << 10

// usageFlushInterval is how often dirty counters are persisted (piggybacks on
// the router watch loop; a crash loses at most this much accounting).
const usageFlushInterval = 30 * time.Second

// usageRetainMonths bounds the persisted history (old windows are pruned).
const usageRetainMonths = 12

type modelUsage struct {
	Requests     int64 `json:"requests"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

// usageMeter accumulates per-model token/request counters per month window.
type usageMeter struct {
	mu        sync.Mutex
	path      string // persistence file ("" = in-memory only)
	dirty     bool
	lastFlush time.Time
	// windows: "2026-07" → model name → counters.
	windows map[string]map[string]*modelUsage
}

func newUsageMeter(configPath string) *usageMeter {
	m := &usageMeter{windows: map[string]map[string]*modelUsage{}}
	if configPath != "" {
		m.path = filepath.Join(filepath.Dir(configPath), "usage.json")
		m.load()
	}
	return m
}

func usageWindowKey(t time.Time) string { return t.UTC().Format("2006-01") }

// record folds one finished upstream response into the current month window.
// in/out may be zero (no usage frame observed) — the request still counts.
func (m *usageMeter) record(model string, in, out int64) {
	if model == "" {
		model = "(none)"
	}
	window := usageWindowKey(time.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.windows[window]
	if w == nil {
		w = map[string]*modelUsage{}
		m.windows[window] = w
		m.pruneLocked()
	}
	u := w[model]
	if u == nil {
		u = &modelUsage{}
		w[model] = u
	}
	u.Requests++
	u.InputTokens += in
	u.OutputTokens += out
	m.dirty = true
}

// pruneLocked drops the oldest windows beyond usageRetainMonths. Caller holds mu.
func (m *usageMeter) pruneLocked() {
	if len(m.windows) <= usageRetainMonths {
		return
	}
	keys := make([]string, 0, len(m.windows))
	for k := range m.windows {
		keys = append(keys, k)
	}
	sort.Strings(keys) // "YYYY-MM" sorts chronologically
	for _, k := range keys[:len(keys)-usageRetainMonths] {
		delete(m.windows, k)
	}
}

func (m *usageMeter) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return // first boot / unreadable — start empty
	}
	var windows map[string]map[string]*modelUsage
	if json.Unmarshal(data, &windows) == nil && windows != nil {
		m.windows = windows
	}
}

// maybeFlush persists dirty counters at most once per usageFlushInterval.
// Called from the router watch loop; also safe to call ad hoc.
func (m *usageMeter) maybeFlush() {
	m.mu.Lock()
	if m.path == "" || !m.dirty || time.Since(m.lastFlush) < usageFlushInterval {
		m.mu.Unlock()
		return
	}
	data, err := json.MarshalIndent(m.windows, "", " ")
	m.dirty = false
	m.lastFlush = time.Now()
	m.mu.Unlock()
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, m.path) // atomic replace; best-effort
	}
}

// snapshot returns the counters for one window, sorted by model name.
type usageRow struct {
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	EstCostUSD   float64 `json:"estCostUsd,omitempty"`
}

func (m *usageMeter) snapshot(window string) []usageRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.windows[window]
	rows := make([]usageRow, 0, len(w))
	for model, u := range w {
		rows = append(rows, usageRow{Model: model, Requests: u.Requests, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Model < rows[j].Model })
	return rows
}

// ---- token extraction from the response byte stream ----

// usageTail tees a bounded tail of the response body and, on EOF/close, hands
// it to done exactly once. The bytes delivered to the client are untouched.
type usageTail struct {
	src      io.ReadCloser
	tail     []byte
	done     func(tail []byte)
	finished bool
}

func newUsageTail(src io.ReadCloser, done func(tail []byte)) *usageTail {
	return &usageTail{src: src, done: done}
}

// Read reads bytes from the wrapped source.
func (u *usageTail) Read(p []byte) (int, error) {
	n, err := u.src.Read(p)
	if n > 0 {
		u.tail = append(u.tail, p[:n]...)
		if over := len(u.tail) - usageTailBytes; over > 0 {
			u.tail = u.tail[over:]
		}
	}
	if err == io.EOF {
		u.finish()
	}
	return n, err
}

// Close releases the wrapped resource.
func (u *usageTail) Close() error {
	u.finish()
	return u.src.Close()
}

func (u *usageTail) finish() {
	if u.finished || u.done == nil {
		return
	}
	u.finished = true
	u.done(u.tail)
}

// usageObjectPattern locates "usage" JSON objects in a response tail. SSE
// streams repeat interim usage frames on some backends; the LAST match is the
// final cumulative count, so parseUsageTail takes the last parseable one.
var usageObjectPattern = regexp.MustCompile(`"usage"\s*:\s*\{`)

// parseUsageTail extracts (inputTokens, outputTokens) from a response tail —
// OpenAI dialect (prompt_tokens/completion_tokens) and Anthropic dialect
// (input_tokens/output_tokens) both parse; missing/garbled usage returns zeros.
func parseUsageTail(tail []byte) (in, out int64) {
	locs := usageObjectPattern.FindAllIndex(tail, -1)
	for i := len(locs) - 1; i >= 0; i-- {
		start := bytes.IndexByte(tail[locs[i][0]:], '{')
		if start < 0 {
			continue
		}
		obj := balancedJSONObject(tail[locs[i][0]+start:])
		if obj == nil {
			continue
		}
		var u struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			InputTokens      int64 `json:"input_tokens"`
			OutputTokens     int64 `json:"output_tokens"`
		}
		if json.Unmarshal(obj, &u) != nil {
			continue
		}
		in = u.PromptTokens + u.InputTokens
		out = u.CompletionTokens + u.OutputTokens
		if in > 0 || out > 0 {
			return in, out
		}
	}
	return 0, 0
}

// balancedJSONObject returns the shortest prefix of b (which must start at
// '{') that forms a brace-balanced object, respecting strings/escapes.
func balancedJSONObject(b []byte) []byte {
	depth := 0
	inString := false
	escaped := false
	for i, c := range b {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return b[:i+1]
			}
		}
	}
	return nil
}

// ---- HTTP surface ----

// usageHandler serves GET /v1/usage: the current month window's per-model
// requests/tokens, plus estimated cost and budget when the config declares
// pricing/budget. Token-gated like the other catalog endpoints.
func (rt *router) usageHandler(w http.ResponseWriter, r *http.Request) {
	if !rt.authed(w, r) {
		return
	}
	window := usageWindowKey(time.Now())
	if q := strings.TrimSpace(r.URL.Query().Get("window")); q != "" {
		window = q
	}
	rows := rt.usage.snapshot(window)

	// Fold in per-entry pricing (config) to estimate cost.
	var totalCost float64
	priced := false
	for i := range rows {
		e, ok := rt.lookup(rows[i].Model)
		if !ok || e.Pricing == nil {
			continue
		}
		priced = true
		cost := float64(rows[i].InputTokens)/1e6*e.Pricing.InputPerMTokUSD +
			float64(rows[i].OutputTokens)/1e6*e.Pricing.OutputPerMTokUSD
		rows[i].EstCostUSD = cost
		totalCost += cost
	}

	resp := map[string]any{
		"window": window,
		"models": rows,
	}
	if priced {
		resp["estCostUsd"] = totalCost
	}
	if b := rt.cur().cfg.MonthlyBudgetUSD; b > 0 {
		budget := map[string]any{"monthlyUsd": b}
		if priced {
			budget["usedPercent"] = totalCost / b
		}
		resp["budget"] = budget
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// modelPricing is a config entry's optional per-token pricing, used only to
// estimate cost in /v1/usage — it never affects routing.
type modelPricing struct {
	InputPerMTokUSD  float64 `json:"inputPerMTokUsd"`
	OutputPerMTokUSD float64 `json:"outputPerMTokUsd"`
}
