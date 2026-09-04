package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// thinkingDisplayKey is where a persisted thinking block carries its Korean
// display copy: inside the block's `metadata`, the code-only sideband that
// survives the transcript and compaction but is dropped by both provider wire
// projections (llm.ContentBlock.Metadata). The reasoning itself is never
// rewritten — the model's own text, and its signature, stay exactly as sent.
const thinkingDisplayKey = "ko"

// thinkingDisplayTimeout bounds the persist path. The stream translated this
// same text seconds ago, so the line cache normally answers instantly; when it
// does not, the row is stored without a display copy rather than holding the
// agent loop open between turns.
const thinkingDisplayTimeout = time.Second

// attachThinkingDisplay adds a Korean display copy to each thinking block of an
// assistant message about to be persisted, so a reloaded conversation shows the
// same Korean the reader watched live. Without it the transcript hands back the
// raw blocks and every restored turn reverts to the model's own language.
//
// The blocks are edited as generic JSON rather than round-tripped through a
// struct: a thinking block carries a provider signature (and may carry fields
// this build has never heard of), and losing one would break the replay that
// feeds the model its own reasoning back.
//
// Fail-open in every direction — unparseable content, a refusal, a timeout — the
// message is persisted exactly as it arrived.
func attachThinkingDisplay(deps runDeps, sessionKey string, raw json.RawMessage, logger *slog.Logger) json.RawMessage {
	if deps.translateThinking == nil || len(raw) == 0 {
		return raw
	}
	// Only conversations a person reopens are worth a display copy. Autonomous
	// runs (cron, goals, dreaming) persist reasoning nobody reads — measured at
	// 8% of a day's thinking text — and translating it is pure spend.
	if !isHumanReadSession(sessionKey) || !showThinkingInChat(deps, sessionKey) {
		return raw
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil || len(blocks) == 0 {
		return raw
	}

	ctx, cancel := context.WithTimeout(context.Background(), thinkingDisplayTimeout)
	defer cancel()

	changed := false
	for _, b := range blocks {
		if t, _ := b["type"].(string); t != "thinking" {
			continue
		}
		// A thinking block usually carries its text under "thinking", but some
		// providers put it in "text" — the reader falls back to both, so the
		// writer has to as well or those rows silently keep their English.
		text, _ := b["thinking"].(string)
		if strings.TrimSpace(text) == "" {
			text, _ = b["text"].(string)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		translated, ok := deps.translateThinking(ctx, text)
		if !ok || strings.TrimSpace(translated) == "" {
			continue
		}
		meta, _ := b["metadata"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
		}
		meta[thinkingDisplayKey] = translated
		b["metadata"] = meta
		changed = true
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(blocks)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to re-encode assistant blocks with the reasoning display copy", "error", err)
		}
		return raw
	}
	return out
}

// isHumanReadSession reports whether a session's transcript is one a person
// reopens in the client. Native conversations are "client:…"; cron, goal,
// heartbeat and system runs keep transcripts for the agent's own continuity.
func isHumanReadSession(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, chatport.NativeClientChannel+":")
}
