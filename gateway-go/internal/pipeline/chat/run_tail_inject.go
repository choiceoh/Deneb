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
// AutoDeliveredOutput is true for cron/scheduled runs AND every interactive
// native-client chat (miniapp_bridge sets it — the reply is returned/pushed,
// not streamed to a blocking SSE waiter). So this directive must read true for
// both: the earlier wording ("이 실행은 예약된 자동 실행이다" + "보고 본문(제목/헤딩)
// 으로 시작") falsely told every interactive chat it was a scheduled run and
// forced report framing, fighting the system prompt's conversational guidance.
// The load-bearing semantics — don't deliver via the message tool, don't
// hallucinate a "channel down" apology — are battle-tested and preserved.
const autoDeliveryDirective = `[전달 정책 — 이번 턴]
- 최종 응답 텍스트는 시스템이 사용자에게 그대로 전달한다. 결과를 전달하려고 ` + "`message`" + ` 도구를 호출하지 마라 — 최종 결과 텍스트를 작성하고 턴을 끝내라.
- 결과(답/본문)부터 바로 시작하라 — "이제 ...를 정리할게요" 같은 사고·전환 문장을 앞에 붙이지 마라.
- 내부 전송 도구가 실패하더라도 그것은 채널 장애가 아니다. "채널이 끊겼다 / 연결되지 않았다 / 복구되면 보내겠다 / 여기 직접 전달한다" 같은 안내를 절대 하지 마라 — 채널은 정상이고 너의 결과물은 그대로 전달된다.`

// chatbotToneDirective is the per-turn behavioral framing for the 챗봇 workspace
// (chat: sessions). The 챗봇/업무 split is otherwise only skin-deep — same single
// persona, same system prompt (deliberately: a per-mode system prompt would split
// the vLLM APC prefix into two families). This nudge differentiates the *tone*
// without touching the system prompt: as a tail addition on the last user message
// it is byte-for-byte APC-safe (see the file header), costing only its own tokens
// on chat: turns. It steers toward light general conversation and, crucially, tells
// the model not to volunteer 업무 (mail/deals/projects/calendar) context unless the
// user asks — that work context still lives in the shared system prompt, so without
// this the 챗봇 answers in full chief-of-staff register.
const chatbotToneDirective = `[대화 모드 — 이번 턴]
- 지금은 가벼운 일반 대화 공간(챗봇)이야. 업무 비서가 아니라 편한 대화 상대로 답해.
- 사용자가 먼저 꺼내지 않으면 업무 맥락(메일·거래처·프로젝트·일정·회사 사정)을 끌어오지 마라. 일반 지식·잡담·코딩 등 질문 그 자체에만 충실하게.
- 보고서식 구조(헤딩·번호 목록)는 요청 없으면 쓰지 말고, 길이는 질문에 맞춰 짧고 자연스럽게.`

// buildTailAdditions collects the per-turn wire-only additions for this run
// in injection order: recall evidence and the 업무 feed digest first (reference
// material), then the 챗봇 tone framing (workspace register), then the delivery
// directive (current-turn policy). Empty strings are omitted.
func buildTailAdditions(params RunParams, recallMemory string) []string {
	var adds []string
	if recallMemory != "" {
		adds = append(adds, recallMemory)
	}
	// 업무 day's-feed digest: the bridge sets this only for 업무 turns, so its
	// presence already gates it to that workspace (same reference-material slot
	// as recall, wire-only on the last user message).
	if params.FeedContext != "" {
		adds = append(adds, params.FeedContext)
	}
	if isChatbotSessionKey(params.SessionKey) {
		adds = append(adds, chatbotToneDirective)
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
