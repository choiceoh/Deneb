// Package toolmeta is the structured metadata sideband for tool results.
//
// A tool result's Content string serves two audiences at once in Deneb: the
// model reads it as the tool outcome, and code re-derives state from embedded
// text markers (deferred-activation notices, spillover pointers). The marker
// half is fragile — writers and parsers must stay in lockstep, and any tool
// output that CONTAINS a marker-shaped string (a read of a log file, a skill
// body, promptware) can forge evidence. This package adds a code-only channel
// alongside the text: values set here are attached to the tool_result block's
// Metadata field by the executor (agentsys/agent.finishToolCall), persist in
// the transcript verbatim, survive compaction stubbing (which replaces only
// Content), and are NEVER sent to a provider — both wire paths project blocks
// through explicit structs that do not copy Metadata.
//
// This is a leaf package so both tool implementations
// (pipeline/chat/tools, via context) and the executor (agentsys/agent) can
// use it without layering violations. Model-facing text markers stay: the
// model still needs the prose ("call them directly", the fence). Metadata is
// the machine half of the same fact.
package toolmeta

import (
	"context"
	"encoding/json"
	"sync"
)

// Collector accumulates metadata for ONE tool call. The executor creates one
// per call and attaches its JSON to the result block; tools and post-
// processors reach it through the call's context. Mutex-guarded so a tool fn
// that fans out internally can still write safely.
type Collector struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

// NewCollector returns an empty per-call collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Set records a metadata value under key, replacing any prior value. Values
// that fail to marshal are dropped silently — metadata is best-effort
// sideband, never worth failing the tool call over.
func (c *Collector) Set(key string, v any) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]json.RawMessage)
	}
	c.m[key] = raw
	c.mu.Unlock()
}

// JSON returns the collected metadata as a JSON object, or nil when nothing
// was set (so the block field stays absent, not `{}`).
func (c *Collector) JSON() json.RawMessage {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) == 0 {
		return nil
	}
	raw, err := json.Marshal(c.m)
	if err != nil {
		return nil
	}
	return raw
}

type ctxKey struct{}

// WithCollector attaches a per-call collector to the context.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// Set records metadata on the current call's collector; no-op when the
// context carries none (tests, callers outside the executor loop).
func Set(ctx context.Context, key string, v any) {
	c, _ := ctx.Value(ctxKey{}).(*Collector)
	c.Set(key, v)
}

// Get decodes one metadata key from a tool_result block's Metadata payload
// into out, reporting whether the key was present and decoded. The reader
// half of Set, shared so consumers don't hand-roll the two-level unmarshal.
func Get(metadata json.RawMessage, key string, out any) bool {
	if len(metadata) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(metadata, &m) != nil {
		return false
	}
	raw, ok := m[key]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}
