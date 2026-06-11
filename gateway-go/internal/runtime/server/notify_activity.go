// notify_activity.go — notifyService in-flight activity tracking: the
// per-session activity cache fed by `agent` / `session.tool` broadcast taps,
// plus the payload field extraction helpers. Split from notify_relay.go
// (pure move).
package server

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
)

// activityMaxSessions caps the per-session activity cache to keep memory
// bounded across long uptimes. When exceeded, the oldest entries are
// evicted on insert. 64 sessions is well above the realistic working set
// for a single-user deployment.
const activityMaxSessions = 64

// activityEntry is the snapshot of in-flight tool activity for one session,
// updated whenever an `agent` (tool.start/tool.end/run.start/run.end) or
// `session.tool` event fires for that session.
type activityEntry struct {
	tool    string    // tool name from the most recent tool.start
	running bool      // true between tool.start and tool.end / run.end
	isError bool      // last tool's error flag (post-result)
	updated time.Time // wall-clock time of the last update
}

// recordActivity updates the in-flight activity cache from broadcast events.
// Three event sources contribute:
//
//   - "agent" — fallback path (no Publisher). Payload is events.AgentEvent
//     struct with Kind in {tool.start, tool.end, run.start, run.end}.
//   - "agent.event" — Publisher-mediated path. Payload is map[string]any
//     with the same fields flattened ({"kind": "...", "sessionKey": "...",
//     "payload": map{...}}).
//   - "session.tool" — fires AFTER a tool result; used to record the
//     final error flag.
//
// All other events are ignored. The function is cheap (single map write)
// and safe to call from the broadcast hot path.
func (n *notifyService) recordActivity(event string, payload any) {
	switch event {
	case "agent", "agent.event":
		n.recordAgentActivity(payload)
	case "session.tool":
		n.recordToolResult(payload)
	}
}

// agentEventFields normalises both AgentEvent struct and its publisher
// map[string]any rendering into the four fields the activity recorder
// needs. Returns ok=false when the payload doesn't carry an actionable
// session key.
func agentEventFields(payload any) (kind, sessionKey, runID string, sub any, ok bool) {
	switch v := payload.(type) {
	case events.AgentEvent:
		if v.SessionKey == "" {
			return "", "", "", nil, false
		}
		return v.Kind, v.SessionKey, v.RunID, v.Payload, true
	case map[string]any:
		sk := stringField(v, "sessionKey")
		if sk == "" {
			return "", "", "", nil, false
		}
		return stringField(v, "kind"), sk, stringField(v, "runId"), v["payload"], true
	default:
		return "", "", "", nil, false
	}
}

func (n *notifyService) recordAgentActivity(payload any) {
	kind, sessionKey, _, sub, ok := agentEventFields(payload)
	if !ok {
		return
	}
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	n.evictIfOversizeLocked()
	entry := n.activity[sessionKey]
	if entry == nil {
		entry = &activityEntry{}
		n.activity[sessionKey] = entry
	}
	entry.updated = time.Now()
	switch kind {
	case "tool.start":
		entry.tool = stringFromAgentPayload(sub, "tool")
		entry.running = true
		entry.isError = false
	case "tool.end":
		entry.running = false
		if b, ok := boolFromAgentPayload(sub, "isError"); ok {
			entry.isError = b
		}
	case "run.start":
		entry.tool = ""
		entry.running = false
		entry.isError = false
	case "run.end":
		entry.running = false
	}
}

func (n *notifyService) recordToolResult(payload any) {
	fields, ok := payload.(map[string]any)
	if !ok {
		return
	}
	sessionKey := stringField(fields, "sessionKey")
	if sessionKey == "" {
		return
	}
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	n.evictIfOversizeLocked()
	entry := n.activity[sessionKey]
	if entry == nil {
		entry = &activityEntry{}
		n.activity[sessionKey] = entry
	}
	entry.tool = stringField(fields, "tool")
	entry.running = false
	if v, ok := fields["isError"]; ok {
		if b, ok := v.(bool); ok {
			entry.isError = b
		}
	}
	entry.updated = time.Now()
}

// evictIfOversizeLocked drops the oldest activity entries when the cache
// exceeds activityMaxSessions. Caller must hold activityMu. O(n) on
// eviction; runs only when the cap is exceeded so amortized cost is low.
func (n *notifyService) evictIfOversizeLocked() {
	if len(n.activity) < activityMaxSessions {
		return
	}
	var oldestKey string
	var oldestT time.Time
	for k, e := range n.activity {
		if oldestKey == "" || e.updated.Before(oldestT) {
			oldestKey = k
			oldestT = e.updated
		}
	}
	if oldestKey != "" {
		delete(n.activity, oldestKey)
	}
}

// activityFor returns a copy of the activity entry for the session, or nil
// if no activity has been recorded. Returning a copy lets the caller render
// without holding the lock.
func (n *notifyService) activityFor(sessionKey string) *activityEntry {
	n.activityMu.Lock()
	defer n.activityMu.Unlock()
	e := n.activity[sessionKey]
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

// stringFromAgentPayload pulls a string field out of AgentEvent.Payload,
// which is a map[string]any in the chat pipeline's emit calls.
func stringFromAgentPayload(p any, key string) string {
	m, ok := p.(map[string]any)
	if !ok {
		return ""
	}
	return stringField(m, key)
}

// boolFromAgentPayload pulls a bool field out of AgentEvent.Payload. The
// second return distinguishes "field absent" (false, false) from "field
// present and false" (false, true) so callers don't unintentionally clear
// a previous error flag.
func boolFromAgentPayload(p any, key string) (value, ok bool) {
	m, isMap := p.(map[string]any)
	if !isMap {
		return false, false
	}
	v, present := m[key]
	if !present {
		return false, false
	}
	b, isBool := v.(bool)
	return b, isBool
}
