// run_midrun_anchor.go closes the mid-run half of the response-language anchor.
//
// The tail anchor (responseLanguageAnchor, run_tail_inject.go) rides the last
// USER message, so on the first LLM step it is the last thing the model reads
// before its own turn marker. From the second step on, the last message is the
// executor's tool-results message: the model's most recent context is then
// whatever the tools returned — transcript excerpts, file dumps, JSON — with the
// anchor one or more tool turns behind it. That is the shape the 2026-09-17
// contamination request described ("the immediate context is English agent
// scaffolding"), and it is the common shape of a long tool loop.
//
// This hook appends the same anchor as a trailing text block to the
// tool-results message in the PER-REQUEST copy (BeforeAPICall, Rule A
// exception 2 in prompt-cache.md). It is never persisted, so history stays
// byte-identical: request N ends with [tool results][anchor], request N+1 with
// [tool results][assistant][tool results][anchor] — the cached prefix through
// the clean tool results is reused and only the anchor's own ~60 tokens are
// recomputed per step. It runs after the trailing cache hook, so the
// cache_control marker stays on the clean block and the anchor block carries
// none (the 4-marker budget is untouched).
//
// Content-prefix cache providers (kimi, prompt-cache.md §1.6) carry it too.
// What §1.6 forbids is MUTATING history mid-run — every byte after the
// mutation goes cold. A per-request trailing block mutates nothing: the next
// request reproduces every persisted message byte-for-byte, and the only loss
// is the one 256-token chunk that held the previous step's anchor — the same
// bounded cost the moving trailing cache_control marker already pays on
// Anthropic. main moved to kimi on 2026-09-18, so excluding it would have left
// the main chat path without the anchor at all.
//
// Where it does NOT run: ephemeral autonomous turns (heartbeat / self-triggers
// keep their own NO_REPLY contract, exactly like the tail anchor).
// DENEB_MIDRUN_ANCHOR=off is the operational kill switch while the model's
// reaction to a trailing user-role note on tool steps is still being observed
// live; the first application per run is logged at Info ("midrun anchor
// active") so a live turn can be checked from the log.
package chat

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// midRunAnchorEnv disables the mid-run anchor when set to off/0/false.
const midRunAnchorEnv = "DENEB_MIDRUN_ANCHOR"

func midRunAnchorDisabledByEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(midRunAnchorEnv))) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// buildMidRunAnchorHook returns the BeforeAPICall hook, or nil when the run
// must not carry it (see the file comment).
func buildMidRunAnchorHook(params RunParams, logger *slog.Logger) func(messages []llm.Message) []llm.Message {
	if params.EphemeralUser || midRunAnchorDisabledByEnv() {
		return nil
	}
	announced := false
	return func(messages []llm.Message) []llm.Message {
		out := appendMidRunAnchor(messages, responseLanguageAnchor)
		if !announced && len(out) > 0 && len(messages) > 0 && &out[0] != &messages[0] {
			announced = true
			if logger != nil {
				logger.Info("midrun anchor active", "session", params.SessionKey, "messages", len(out))
			}
		}
		return out
	}
}

// appendMidRunAnchor returns messages with anchor appended as a trailing text
// block when the LAST message is the executor's tool-results user message.
// Any other tail — the real user turn (the tail anchor is already there), an
// assistant message, an empty list — passes through unchanged. Copy-on-write:
// the input slice and its elements are never mutated. Idempotent: a tail that
// already ends with the anchor is left alone.
func appendMidRunAnchor(messages []llm.Message, anchor string) []llm.Message {
	n := len(messages)
	if n == 0 || anchor == "" {
		return messages
	}
	last := messages[n-1]
	if last.Role != "user" || !hasToolResultBlock(last) || lastTextBlock(last) == anchor {
		return messages
	}
	appended, ok := appendTextToMessage(last, []string{anchor})
	if !ok {
		return messages
	}
	out := make([]llm.Message, n)
	copy(out, messages)
	out[n-1] = appended
	return out
}

func hasToolResultBlock(msg llm.Message) bool {
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

func lastTextBlock(msg llm.Message) string {
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil || len(blocks) == 0 {
		return ""
	}
	if b := blocks[len(blocks)-1]; b.Type == "text" {
		return b.Text
	}
	return ""
}
