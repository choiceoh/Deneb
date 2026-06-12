package streaming

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// BroadcastRawFunc sends pre-serialized event data to all matching subscribers.
type BroadcastRawFunc func(event string, data []byte) int

// Stream event names matching the TypeScript wire format. Exported so
// transport adapters (e.g. the miniapp SSE bridge's BroadcastRawFunc filter)
// can match events without duplicating the strings.
const (
	EventChat     = "chat"
	EventDelta    = "chat.delta"
	EventTool     = "chat.tool"
	EventThinking = "chat.thinking"
)

// Limits for broadcast payloads.
const (
	maxBroadcastResultLen = 4096
)

// thinkingThrottle bounds EmitThinking's broadcast rate. The agent loop fires
// OnThinking once per reasoning delta — potentially hundreds per turn — while
// subscribers only need a liveness signal plus a slowly-updating preview, so
// anything beyond one frame every couple of seconds is noise on the wire.
const thinkingThrottle = 2 * time.Second

// Thinking preview sizing: the tail buffer keeps enough recent reasoning text
// to survive whitespace collapsing; the preview itself is chip-sized (the
// native client renders it as "깊이 생각 중: …<preview>" in the waiting chip).
const (
	thinkingTailRunes    = 512
	thinkingPreviewRunes = 64
	// thinkingPreviewMinRunes suppresses the preview until enough reasoning
	// accumulated to read as a phrase — the first pulse fires on the very
	// first delta, and "깊이 생각 중: The" is worse than the bare indicator.
	thinkingPreviewMinRunes = 12
)

// Broadcaster relays agent streaming events to WebSocket clients
// via the gateway's raw broadcast function. All methods are safe to call
// when broadcastRaw is nil (they silently no-op).
type Broadcaster struct {
	broadcastRaw BroadcastRawFunc
	sessionKey   string
	clientRunID  string
	seq          atomic.Int64
	// lastThinkingNs is the monotonic-ish wall clock (UnixNano) of the last
	// EmitThinking broadcast, used to throttle the per-reasoning-delta firehose.
	lastThinkingNs atomic.Int64
	// thinkingMu guards thinkingTail, the rolling tail of recent reasoning text
	// that throttled EmitThinking frames condense into a chip-sized preview.
	thinkingMu   sync.Mutex
	thinkingTail []rune
}

// NewBroadcaster creates a new Broadcaster for a given session/run.
func NewBroadcaster(broadcastRaw BroadcastRawFunc, sessionKey, clientRunID string) *Broadcaster {
	return &Broadcaster{
		broadcastRaw: broadcastRaw,
		sessionKey:   sessionKey,
		clientRunID:  clientRunID,
	}
}

// EmitDelta broadcasts a streaming text delta to WS clients.
func (sb *Broadcaster) EmitDelta(text string) {
	if text == "" {
		return
	}
	sb.emit(EventDelta, map[string]any{
		"delta": text,
	})
}

// EmitThinking broadcasts a reasoning-in-progress liveness signal, throttled
// to at most one frame per thinkingThrottle. delta is the reasoning text chunk
// that triggered the pulse: every chunk is accumulated into a rolling tail, and
// each throttled frame carries a chip-sized `preview` of the most recent
// reasoning so the client's "깊이 생각 중" indicator can narrate the live
// thought instead of sitting static for the whole reasoning stretch.
func (sb *Broadcaster) EmitThinking(delta string) {
	sb.appendThinking(delta)
	now := time.Now().UnixNano()
	last := sb.lastThinkingNs.Load()
	if now-last < int64(thinkingThrottle) {
		return
	}
	// CAS so concurrent reasoning deltas can't double-emit within one window.
	if !sb.lastThinkingNs.CompareAndSwap(last, now) {
		return
	}
	payload := map[string]any{}
	if preview := sb.thinkingPreview(); preview != "" {
		payload["preview"] = preview
	}
	sb.emit(EventThinking, payload)
}

// appendThinking adds a reasoning chunk to the rolling tail, trimming the
// front so the buffer never exceeds thinkingTailRunes.
func (sb *Broadcaster) appendThinking(delta string) {
	if delta == "" {
		return
	}
	sb.thinkingMu.Lock()
	defer sb.thinkingMu.Unlock()
	sb.thinkingTail = append(sb.thinkingTail, []rune(delta)...)
	if over := len(sb.thinkingTail) - thinkingTailRunes; over > 0 {
		sb.thinkingTail = sb.thinkingTail[over:]
	}
}

// thinkingPreview condenses the rolling tail into a single chip-sized line:
// whitespace collapsed, truncated from the front (the most recent thought is
// the interesting part) with a leading ellipsis when cut.
func (sb *Broadcaster) thinkingPreview() string {
	sb.thinkingMu.Lock()
	tail := string(sb.thinkingTail)
	sb.thinkingMu.Unlock()
	collapsed := strings.Join(strings.Fields(tail), " ")
	runes := []rune(collapsed)
	if len(runes) < thinkingPreviewMinRunes {
		return ""
	}
	if len(runes) <= thinkingPreviewRunes {
		return collapsed
	}
	return "…" + string(runes[len(runes)-thinkingPreviewRunes:])
}

// EmitToolStart broadcasts a tool invocation start event. detail is an
// optional short human hint extracted from the tool input (query, command,
// file name) — omitted from the payload when empty.
func (sb *Broadcaster) EmitToolStart(name, toolUseID, detail string) {
	payload := map[string]any{
		"state":     "started",
		"tool":      name,
		"toolUseId": toolUseID,
	}
	if detail != "" {
		payload["detail"] = detail
	}
	sb.emit(EventTool, payload)
}

// EmitToolResult broadcasts a tool execution result event.
func (sb *Broadcaster) EmitToolResult(name, toolUseID, result string, isError bool) {
	sb.emit(EventTool, map[string]any{
		"state":     "completed",
		"tool":      name,
		"toolUseId": toolUseID,
		"result":    truncateForBroadcast(result, maxBroadcastResultLen),
		"isError":   isError,
	})
}

// EmitComplete broadcasts the final chat completion event.
func (sb *Broadcaster) EmitComplete(text string, usage llm.TokenUsage) {
	sb.emit(EventChat, map[string]any{
		"state": "done",
		"text":  text,
		"usage": map[string]int{
			"inputTokens":  usage.InputTokens,
			"outputTokens": usage.OutputTokens,
		},
	})
}

// EmitError broadcasts an error event for the run.
func (sb *Broadcaster) EmitError(errMsg string) {
	sb.emit(EventChat, map[string]any{
		"state": "error",
		"error": errMsg,
	})
}

// EmitStarted broadcasts that the agent run has started.
func (sb *Broadcaster) EmitStarted() {
	sb.emit(EventChat, map[string]any{
		"state": "started",
	})
}

// EmitAborted broadcasts that the agent run was aborted.
func (sb *Broadcaster) EmitAborted(partialText string) {
	sb.emit(EventChat, map[string]any{
		"state": "aborted",
		"text":  partialText,
	})
}

// emit is the shared broadcast path. It injects common fields (sessionKey,
// clientRunId, seq) and serializes to JSON. No-ops when broadcastRaw is nil.
func (sb *Broadcaster) emit(event string, payload map[string]any) {
	if sb.broadcastRaw == nil {
		return
	}
	payload["sessionKey"] = sb.sessionKey
	payload["clientRunId"] = sb.clientRunID
	payload["seq"] = sb.seq.Add(1)
	data, err := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
	})
	if err != nil {
		return
	}
	sb.broadcastRaw(event, data)
}

// truncateForBroadcast caps a string to at most maxLen bytes to prevent
// oversized WS frames. The cut is rounded back to a UTF-8 rune boundary so
// multi-byte characters (notably Korean) never split across the truncation.
func truncateForBroadcast(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "... [truncated]"
}
