// Node event relay: processes events from connected nodes (mobile/desktop apps).
//
// Mirrors the handleNodeEvent logic from src/gateway/server-node-events.ts.
// Handles voice transcripts, agent requests, notification changes, exec events,
// and chat subscribe/unsubscribe.
package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	maxExecEventOutputChars       = 180
	maxNotificationEventTextChars = 120
	voiceTranscriptDedupeWindowMs = 1500
	maxRecentVoiceTranscripts     = 200
	maxVoiceTranscriptTextLen     = 20_000
	maxAgentRequestMessageLen     = 20_000
)

// NodeEvent represents an event received from a connected node.
type NodeEvent struct {
	Event       string `json:"event"`
	PayloadJSON string `json:"payloadJSON,omitempty"`
}

// NodeEventContext provides dependencies for node event handling.
type NodeEventContext struct {
	Broadcaster *Broadcaster
	Logger      *slog.Logger
}

// voiceTranscriptEntry tracks recent voice transcripts for deduplication.
type voiceTranscriptEntry struct {
	fingerprint string
	ts          int64
}

// voiceDeduper deduplicates voice transcripts within a time window.
type voiceDeduper struct {
	mu      sync.Mutex
	entries map[string]voiceTranscriptEntry
}

var globalVoiceDeduper = &voiceDeduper{
	entries: make(map[string]voiceTranscriptEntry),
}

func (d *voiceDeduper) shouldDrop(sessionKey, fingerprint string, now int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	prev, ok := d.entries[sessionKey]
	if ok && prev.fingerprint == fingerprint && now-prev.ts <= voiceTranscriptDedupeWindowMs {
		return true
	}
	d.entries[sessionKey] = voiceTranscriptEntry{fingerprint: fingerprint, ts: now}

	if len(d.entries) > maxRecentVoiceTranscripts {
		cutoff := now - voiceTranscriptDedupeWindowMs*2
		for key, entry := range d.entries {
			if entry.ts < cutoff {
				delete(d.entries, key)
			}
			if len(d.entries) <= maxRecentVoiceTranscripts {
				break
			}
		}
	}
	return false
}

// HandleNodeEvent processes an event from a connected node.
func HandleNodeEvent(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	switch evt.Event {
	case "voice.transcript":
		handleVoiceTranscript(ctx, nodeID, evt)
	case "agent.request":
		handleAgentRequest(ctx, nodeID, evt)
	case "notifications.changed":
		handleNotificationsChanged(ctx, nodeID, evt)
	case "exec.started", "exec.finished", "exec.denied":
		handleExecEvent(ctx, nodeID, evt)
	case "chat.subscribe":
		handleChatSubscribe(ctx, nodeID, evt)
	case "chat.unsubscribe":
		handleChatUnsubscribe(ctx, nodeID, evt)
	default:
		// Unknown events are silently ignored.
	}
}

func handleVoiceTranscript(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	obj := parsePayloadObject(evt.PayloadJSON)
	if obj == nil {
		return
	}
	text := normalizeNonEmptyString(obj, "text")
	if text == "" || len(text) > maxVoiceTranscriptTextLen {
		return
	}
	sessionKey := normalizeNonEmptyString(obj, "sessionKey")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("node-%s", nodeID)
	}

	fingerprint := resolveVoiceTranscriptFingerprint(obj, text)
	now := time.Now().UnixMilli()
	if globalVoiceDeduper.shouldDrop(sessionKey, fingerprint, now) {
		return
	}

	ctx.Broadcaster.Broadcast("node.voice.transcript", map[string]any{
		"nodeId":     nodeID,
		"sessionKey": sessionKey,
		"text":       text,
	})
}

func handleAgentRequest(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	if evt.PayloadJSON == "" {
		return
	}
	obj := parsePayloadObject(evt.PayloadJSON)
	if obj == nil {
		return
	}
	message := normalizeNonEmptyString(obj, "message")
	if message == "" || len(message) > maxAgentRequestMessageLen {
		return
	}
	sessionKey := normalizeNonEmptyString(obj, "sessionKey")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("node-%s", nodeID)
	}

	ctx.Broadcaster.Broadcast("node.agent.request", map[string]any{
		"nodeId":     nodeID,
		"sessionKey": sessionKey,
		"message":    message,
		"payload":    obj,
	})
}

func handleNotificationsChanged(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	obj := parsePayloadObject(evt.PayloadJSON)
	if obj == nil {
		return
	}
	change := strings.ToLower(normalizeNonEmptyString(obj, "change"))
	if change != "posted" && change != "removed" {
		return
	}
	key := normalizeNonEmptyString(obj, "key")
	if key == "" {
		return
	}

	sessionKey := normalizeNonEmptyString(obj, "sessionKey")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("node-%s", nodeID)
	}
	packageName := normalizeNonEmptyString(obj, "packageName")
	title := compactText(normalizeNonEmptyString(obj, "title"), maxNotificationEventTextChars)
	text := compactText(normalizeNonEmptyString(obj, "text"), maxNotificationEventTextChars)

	summary := fmt.Sprintf("Notification %s (node=%s key=%s", change, nodeID, key)
	if packageName != "" {
		summary += fmt.Sprintf(" package=%s", packageName)
	}
	summary += ")"
	if change == "posted" {
		parts := filterNonEmpty(title, text)
		if len(parts) > 0 {
			summary += ": " + strings.Join(parts, " - ")
		}
	}

	ctx.Broadcaster.Broadcast("node.notification", map[string]any{
		"nodeId":     nodeID,
		"sessionKey": sessionKey,
		"summary":    summary,
		"change":     change,
		"key":        key,
	})
}

func handleExecEvent(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	obj := parsePayloadObject(evt.PayloadJSON)
	if obj == nil {
		return
	}
	sessionKey := normalizeNonEmptyString(obj, "sessionKey")
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("node-%s", nodeID)
	}

	runID := normalizeNonEmptyString(obj, "runId")
	command := normalizeNonEmptyString(obj, "command")
	reason := normalizeNonEmptyString(obj, "reason")
	output := normalizeNonEmptyString(obj, "output")
	exitCode := normalizeFiniteNumber(obj, "exitCode")
	timedOut, _ := obj["timedOut"].(bool)

	var text string
	switch evt.Event {
	case "exec.started":
		text = fmt.Sprintf("Exec started (node=%s", nodeID)
		if runID != "" {
			text += fmt.Sprintf(" id=%s", runID)
		}
		text += ")"
		if command != "" {
			text += ": " + command
		}
	case "exec.finished":
		exitLabel := fmt.Sprintf("code %v", exitCode)
		if timedOut {
			exitLabel = "timeout"
		}
		compactOutput := compactText(output, maxExecEventOutputChars)
		shouldNotify := timedOut || (exitCode != nil && *exitCode != 0) || compactOutput != ""
		if !shouldNotify {
			return
		}
		text = fmt.Sprintf("Exec finished (node=%s", nodeID)
		if runID != "" {
			text += fmt.Sprintf(" id=%s", runID)
		}
		text += fmt.Sprintf(", %s)", exitLabel)
		if compactOutput != "" {
			text += "\n" + compactOutput
		}
	case "exec.denied":
		text = fmt.Sprintf("Exec denied (node=%s", nodeID)
		if runID != "" {
			text += fmt.Sprintf(" id=%s", runID)
		}
		if reason != "" {
			text += ", " + reason
		}
		text += ")"
		if command != "" {
			text += ": " + command
		}
	}

	ctx.Broadcaster.Broadcast("node.exec", map[string]any{
		"nodeId":     nodeID,
		"sessionKey": sessionKey,
		"event":      evt.Event,
		"text":       text,
	})
}

func handleChatSubscribe(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	if evt.PayloadJSON == "" {
		return
	}
	sessionKey := parseSessionKeyFromPayload(evt.PayloadJSON)
	if sessionKey == "" {
		return
	}
	ctx.Broadcaster.SubscribeNodeSession(nodeID, sessionKey)
}

func handleChatUnsubscribe(ctx *NodeEventContext, nodeID string, evt NodeEvent) {
	if evt.PayloadJSON == "" {
		return
	}
	sessionKey := parseSessionKeyFromPayload(evt.PayloadJSON)
	if sessionKey == "" {
		return
	}
	ctx.Broadcaster.UnsubscribeNodeSession(nodeID, sessionKey)
}

// Helper functions.

func parsePayloadObject(payloadJSON string) map[string]any {
	if payloadJSON == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &obj); err != nil {
		return nil
	}
	return obj
}

func parseSessionKeyFromPayload(payloadJSON string) string {
	obj := parsePayloadObject(payloadJSON)
	if obj == nil {
		return ""
	}
	return normalizeNonEmptyString(obj, "sessionKey")
}

func normalizeNonEmptyString(obj map[string]any, key string) string {
	v, ok := obj[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func normalizeFiniteNumber(obj map[string]any, key string) *float64 {
	v, ok := obj[key]
	if !ok {
		return nil
	}
	n, ok := v.(float64)
	if !ok {
		return nil
	}
	return &n
}

func compactText(raw string, maxLen int) string {
	normalized := strings.Join(strings.Fields(raw), " ")
	if normalized == "" {
		return ""
	}
	if len(normalized) <= maxLen {
		return normalized
	}
	safe := maxLen - 1
	if safe < 1 {
		safe = 1
	}
	return normalized[:safe] + "…"
}

func resolveVoiceTranscriptFingerprint(obj map[string]any, text string) string {
	eventID := firstNonEmpty(
		normalizeNonEmptyString(obj, "eventId"),
		normalizeNonEmptyString(obj, "providerEventId"),
		normalizeNonEmptyString(obj, "transcriptId"),
	)
	if eventID != "" {
		return "event:" + eventID
	}

	callID := firstNonEmpty(
		normalizeNonEmptyString(obj, "providerCallId"),
		normalizeNonEmptyString(obj, "callId"),
	)
	seq := normalizeFiniteNumber(obj, "sequence")
	if seq == nil {
		seq = normalizeFiniteNumber(obj, "seq")
	}
	if callID != "" && seq != nil {
		return fmt.Sprintf("call-seq:%s:%d", callID, int64(*seq))
	}

	ts := normalizeFiniteNumber(obj, "timestamp")
	if ts == nil {
		ts = normalizeFiniteNumber(obj, "ts")
	}
	if ts == nil {
		ts = normalizeFiniteNumber(obj, "eventTimestamp")
	}
	if callID != "" && ts != nil {
		return fmt.Sprintf("call-ts:%s:%d", callID, int64(*ts))
	}
	if ts != nil {
		return fmt.Sprintf("timestamp:%d|text:%s", int64(*ts), text)
	}
	return "text:" + text
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func filterNonEmpty(values ...string) []string {
	var result []string
	for _, v := range values {
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}
