// grace.go — "Grace call" mechanism for max-turn exhaustion.
//
// When an agent hits its turn budget mid-task (still calling tools), abruptly
// terminating leaves a "half-finished" trailing experience: pending tool
// results with no assistant follow-up. Instead, we give the model exactly ONE
// more turn with an injected user message asking it to wrap up (summarize,
// apologize, save state) and then exit cleanly.
//
// Design decisions (ported from Hermes agent, see docs/research):
//
//  1. Only ONE grace turn. The injection happens once, the flag is cleared
//     after the grace iteration completes. No cascading grace-on-grace.
//  2. No pre-warning. We do NOT tell the model "budget is getting low"
//     earlier in the run — that caused models to "give up" prematurely on
//     complex tasks. The injection only fires on actual exhaustion.
//  3. Plain user message. The grace prompt is a standard user-role message
//     appended to the history. Prompt cache up to the last real user turn
//     is preserved (we only ever append, never mutate prior messages).
//  4. Korean-first. Matches the Deneb Korean-first output rule.
package agent

// GraceCallPrompt is the user-role message injected on the single grace turn
// after MaxTurns is exhausted. The model is told the budget is spent and
// asked to produce a terminal text response — no tools, no continuation.
//
// Kept short and directive: the model already has full context from the
// preceding turns; we only need to flip its posture from "keep working" to
// "close out cleanly".
const GraceCallPrompt = "[시스템 안내] 에이전트 턴 예산 한도에 도달했습니다. " +
	"추가 도구 호출은 하지 말고, 지금까지의 진행 상황을 짧게 요약해서 " +
	"한국어로 답변하세요. 완료하지 못한 작업이 있다면 현재 상태와 " +
	"다음 단계 제안만 명확히 알려주세요."

// StopReasonMaxTurnsGraceful is the terminal stop reason set when an agent
// run exits after the one-shot grace turn injected upon MaxTurns exhaustion.
// Callers can distinguish this from plain "max_turns" (which would only
// appear now if the grace iteration itself errors out before producing text).
const StopReasonMaxTurnsGraceful = "max_turns_graceful"
