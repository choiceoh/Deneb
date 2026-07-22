// run_thinking_summary.go — Option A for the live "thinking" chip: a fast local
// (tiny) model turns the streamed reasoning tail into a one-line Korean progress
// label ("메일 3건 대조 중"), replacing the deterministic latest-sentence chip
// that surfaced raw — and often non-Korean (kimi/glm reason in en/zh) — model
// self-talk. The refiner runs off the broadcaster's hot path (async,
// one-in-flight, subscriber-gated); see streaming.Broadcaster.EmitThinking.
package chat

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

const (
	// thinkingSummaryTimeout bounds one chip-summary model call. The tiny model
	// returns ~a dozen tokens; if it can't inside this window the frame is dropped
	// (the deterministic chip already shipped) rather than letting stale summaries
	// pile up behind the ~2s throttle.
	thinkingSummaryTimeout = 1500 * time.Millisecond
	// thinkingSummaryMaxTokens caps the label generation — one fuller line (the
	// chip now renders on up to two lines on both clients, so the model has room
	// to name the object/condition, not just the verb).
	thinkingSummaryMaxTokens = 96
	// thinkingSummaryMinTailRunes gates the call until enough reasoning has
	// accumulated to name a step; below it the deterministic chip is fine and a
	// model call would just paraphrase a fragment.
	thinkingSummaryMinTailRunes = 40
	// thinkingSummaryMaxRunes caps the rendered chip line — a safety net above the
	// prompt's ~40-char target so a slightly-over line shows in full (two lines)
	// rather than being hard-cut mid-phrase.
	thinkingSummaryMaxRunes = 60
)

// thinkingSummarySystem instructs the tiny model to name the CURRENT step rather
// than transcribe the reasoning, in one Korean progress line. The chip renders on
// up to two lines, so the line should carry the object/condition ("무엇을·어떤 대상")
// for a readable "what it's doing now", not just a bare verb.
const thinkingSummarySystem = "너는 AI 비서 화면의 '생각 중' 표시줄을 채운다. 아래 텍스트는 비서가 사용자 요청을 처리하며 지금 진행 중인 내부 추론이다. 지금 무엇을 하는 중인지 한국어로 한 줄로 요약해라. 규칙: '무엇을·어떤 대상까지' 담아 조금 구체적인 진행형으로('발신자 주소와 과거 거래 이력을 대조하는 중', '지난 3개월 거래 내역과 계좌 변경 여부를 확인하는 중'), 40자 이내, 따옴표·마침표·설명·목록 없이 요약만. 추론을 그대로 옮기지 말고 '지금 하는 일'로 압축."

// newThinkingSummarizer builds the Option-A chip refiner. It returns nil when
// local AI is not wired, so the broadcaster keeps the deterministic preview. The
// returned fn is invoked off the broadcaster's hot path (async, one-in-flight);
// it derives a short per-call deadline from ctx so a slow model never outlives
// the throttle window or the run.
func newThinkingSummarizer(ctx context.Context) func(reasoningTail string) (string, bool) {
	if pilot.LocalAIHub() == nil {
		return nil
	}
	return func(tail string) (string, bool) {
		tail = strings.TrimSpace(tail)
		if len([]rune(tail)) < thinkingSummaryMinTailRunes {
			return "", false
		}
		if pilot.LocalAIRecentlyDown() {
			return "", false
		}
		cctx, cancel := context.WithTimeout(ctx, thinkingSummaryTimeout)
		defer cancel()
		out, err := pilot.CallTinyLLM(cctx, thinkingSummarySystem, tail, thinkingSummaryMaxTokens)
		if err != nil {
			return "", false
		}
		line := cleanThinkingSummaryLine(out)
		if line == "" {
			return "", false
		}
		return line, true
	}
}

// cleanThinkingSummaryLine reduces the tiny model's output to one chip line:
// first non-empty line, wrapper/quote/markdown punctuation stripped, trailing
// sentence punctuation dropped, capped to thinkingSummaryMaxRunes runes.
func cleanThinkingSummaryLine(s string) string {
	line := ""
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			line = t
			break
		}
	}
	line = strings.Trim(line, " \t\"'`“”‘’*#>-·•[]()")
	line = strings.TrimRight(line, " .!?。！？…")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > thinkingSummaryMaxRunes {
		line = strings.TrimSpace(string(runes[:thinkingSummaryMaxRunes]))
	}
	return line
}
