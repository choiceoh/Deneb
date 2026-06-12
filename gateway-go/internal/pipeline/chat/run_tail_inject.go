// run_tail_inject.go appends per-turn wire-only additions (recall evidence,
// the auto-delivery directive) to the LAST user message of the assembled
// message list instead of the system prompt.
//
// Why the tail and not the system prompt: the main chat model is served by
// local vLLM, whose automatic prefix caching (APC) is strict byte-prefix
// matching over the rendered prompt [system][tool schemas][history]. Any
// per-turn bytes in the system prompt therefore invalidate the KV cache of
// the tool schemas AND the entire conversation history on every turn.
// Measured on DSV4-Flash (2026-06-13): 80.7% token hit rate with a 20-40s
// prefill tail on interactive turns, traced to the recall block (hindsight
// auto-recall runs every turn) being appended to the system prompt. At the
// message tail the same bytes cost only themselves: the stable
// system+tools+history prefix stays cached.
//
// Wire-only: the transcript persisted the clean user message before the run,
// and the injection operates on a copied message slice, so the next turn's
// history reload is byte-identical to the prefix this turn wrote into the
// KV cache. (Within the run, the agent loop appends assistant/tool messages
// after the injected message, so the model sees the additions on every step
// of the run — same visibility as the old system placement.)
//
// See .claude/rules/prompt-cache.md ("vLLM APC") for the doctrine.
package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// autoDeliveryDirective is the per-turn delivery policy for runs whose final
// reply text is delivered by the run-completion layer (cron relay /
// main-session handoff / miniapp sync response) rather than by the agent's
// own `message` tool. It used to live in the system prompt's Messaging
// section gated on RunParams.AutoDeliveredOutput, which split heartbeat
// (flag off) and interactive (flag on) turns of the same session into two
// divergent system prompts — two APC prefix families. As a tail addition it
// costs ~100 tokens on exactly the turns it applies to and the system prompt
// stays byte-identical across both run families.
//
// Wording preserved from the old system-prompt block — the semantics are
// battle-tested against "channel down" hallucinations (see git history of
// prompt/system_prompt.go).
const autoDeliveryDirective = `[전달 정책 — 이번 턴]
- **이 실행은 예약된 자동 실행이다.** 최종 응답 텍스트는 시스템이 자동으로 사용자 채널에 전달한다. 결과를 전달하려고 ` + "`message`" + ` 도구를 호출하지 마라 — 그냥 최종 결과 텍스트를 작성하고 턴을 끝내라.
- 최종 텍스트는 그대로 사용자에게 보이는 배달문이다. 보고 본문(제목/헤딩)으로 바로 시작하라 — "이제 분석 보고를 정리해" 같은 사고·전환 문장을 본문 앞에 붙이지 마라.
- 내부 전송 도구가 실패하더라도 그것은 채널 장애가 아니다. "채널이 끊겼다 / 연결되지 않았다 / 복구되면 보내겠다 / 여기 직접 전달한다" 같은 안내를 절대 하지 마라 — 채널은 정상이고 너의 결과물은 그대로 전달된다.`

// buildTailAdditions collects the per-turn wire-only additions for this run
// in injection order: recall evidence first (reference material), then the
// delivery directive (current-turn policy). Empty strings are omitted.
func buildTailAdditions(params RunParams, recallMemory string) []string {
	var adds []string
	if recallMemory != "" {
		adds = append(adds, recallMemory)
	}
	if params.AutoDeliveredOutput {
		adds = append(adds, autoDeliveryDirective)
	}
	return adds
}

// injectTailAdditions appends the additions to the last user message of
// messages, returning a copied slice (the input is never mutated — it may
// alias transcript-backed state). Returns injected=false when there is
// nothing to add or no user message exists to carry the additions; the
// caller falls back to the legacy system-prompt placement in that case so
// evidence is never silently dropped.
func injectTailAdditions(messages []llm.Message, additions []string) ([]llm.Message, bool) {
	if len(additions) == 0 {
		return messages, true // nothing to add — trivially "done"
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		appended, ok := appendTextToMessage(messages[i], additions)
		if !ok {
			return messages, false
		}
		out := make([]llm.Message, len(messages))
		copy(out, messages)
		out[i] = appended
		return out, true
	}
	return messages, false
}

// appendTextToMessage returns a copy of msg with the additions appended as
// trailing text, handling both content shapes (plain string and content-block
// array; a text block is appended for the latter so image/document blocks
// stay untouched). ok=false for content shapes it cannot extend.
func appendTextToMessage(msg llm.Message, additions []string) (llm.Message, bool) {
	suffix := ""
	for _, a := range additions {
		suffix += "\n\n" + a
	}

	// Plain string content.
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		if s == "" {
			// Degenerate empty message: skip the leading separator.
			return llm.NewTextMessage(msg.Role, suffix[2:]), true
		}
		return llm.NewTextMessage(msg.Role, s+suffix), true
	}

	// Content-block array (multimodal user message): append one text block.
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil && len(blocks) > 0 {
		out := make([]llm.ContentBlock, len(blocks), len(blocks)+1)
		copy(out, blocks)
		out = append(out, llm.ContentBlock{Type: "text", Text: suffix[2:]})
		return llm.NewBlockMessage(msg.Role, out), true
	}

	return msg, false
}
