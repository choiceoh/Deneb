// open_loops.go — prospective memory: extract unfulfilled commitments.
//
// Everything else in the memory system is retrospective (find what happened).
// A chief-of-staff's real value includes the forward direction: "we promised
// the quote by next week", "김 부장 is supposed to call back" — remembered and
// chased. Each dream cycle runs a small, focused extraction pass over the same
// diary/MEMORY.md input and hands the found commitments to a wired sink (the
// gateway wires the local to-do store, which the native client renders and the
// heartbeat signal engine scores for due-soon escalation).
//
// The extraction is a separate LLM call on purpose: the wiki-synthesis JSON
// contract has a history of drift-induced parse failures, and a failed loop
// extraction must never cost a wiki consolidation cycle (and vice versa).
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// OpenLoop is one unfulfilled commitment found in diary/memory content.
type OpenLoop struct {
	What    string `json:"what"`              // the commitment, short imperative Korean
	Who     string `json:"who,omitempty"`     // owner/counterparty ("" = us)
	Due     string `json:"due,omitempty"`     // YYYY-MM-DD when stated, else ""
	Context string `json:"context,omitempty"` // one-line source context
}

// openLoopMaxPerCycle caps extraction output; a cycle that "finds" dozens of
// promises is hallucinating, not remembering.
const openLoopMaxPerCycle = 8

// openLoopMaxTokens bounds the extraction response.
const openLoopMaxTokens = 1024

// openLoopTimeout bounds the extraction LLM call so a wedged backend costs
// the loop pass, not the remaining dream-cycle budget.
const openLoopTimeout = 2 * time.Minute

// SetOpenLoopSink wires the destination for extracted commitments. The sink
// returns how many were newly recorded (after its own dedup). nil disables
// extraction entirely.
func (wd *WikiDreamer) SetOpenLoopSink(fn func(ctx context.Context, loops []OpenLoop) (int, error)) {
	wd.openLoopSink = fn
}

// extractOpenLoops runs the focused extraction pass over the cycle input.
func (wd *WikiDreamer) extractOpenLoops(ctx context.Context, content string) ([]OpenLoop, error) {
	if wd.client == nil || strings.TrimSpace(content) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, openLoopTimeout)
	defer cancel()

	prompt := fmt.Sprintf(`아래 일지/메모에서 **아직 이행되지 않은 약속·후속조치(오픈루프)**만 추출하세요.

## 추출 기준
- 우리(또는 상대)가 하기로 했지만 완료 기록이 없는 것: 회신, 견적 발송, 자료 전달, 확인, 결제, 방문 등
- 같은 내용에서 이미 "완료/보냈음/처리됨"이 확인되면 제외
- 잡담·감상·이미 끝난 일은 제외
- 최대 %d개, 확신 있는 것만

## 출력 (JSON 배열만, 다른 텍스트 없이)
[{"what":"한 줄 약속(한국어 명령형)","who":"주체(우리면 생략)","due":"YYYY-MM-DD(언급된 경우만)","context":"근거 한 줄"}]
오픈루프가 없으면 [] 를 반환하세요.

## 일지/메모
%s`, openLoopMaxPerCycle, content)

	systemJSON, _ := json.Marshal("You extract unfulfilled commitments. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: openLoopMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("open-loop LLM call: %w", err)
	}
	return parseOpenLoops(resp)
}

// parseOpenLoops decodes the extraction response: fences stripped, capped,
// empty entries dropped, free text redacted.
func parseOpenLoops(text string) ([]OpenLoop, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	if text == "" {
		return nil, nil
	}
	var loops []OpenLoop
	if err := json.Unmarshal([]byte(text), &loops); err != nil {
		return nil, fmt.Errorf("parse open loops: %w (raw: %.200s)", err, text)
	}
	out := loops[:0]
	for _, l := range loops {
		l.What = strings.TrimSpace(redact.String(l.What))
		l.Who = strings.TrimSpace(redact.String(l.Who))
		l.Context = strings.TrimSpace(redact.String(l.Context))
		l.Due = strings.TrimSpace(l.Due)
		if l.What == "" {
			continue
		}
		out = append(out, l)
		if len(out) >= openLoopMaxPerCycle {
			break
		}
	}
	return out, nil
}
