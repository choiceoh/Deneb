package anomalywatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// systemPrompt steers the local model toward the one job it is good at here:
// noticing that something in a log window looks off. Every instruction below is
// aimed at the failure mode that makes such a lane worthless — findings a
// reader cannot check.
const systemPrompt = `너는 Deneb 게이트웨이의 런타임 관측자다. 최근 로그 창을 읽고 **이상해 보이는 것**을 보고한다.

원칙:
- 근거 없는 판정은 쓰지 마라. 모든 발견은 창 안에 실제로 있는 로그 줄을 그대로 인용해야 한다. 인용할 줄이 없으면 그 발견은 없는 것이다.
- 너는 조치하지 않는다. 관측만 기록한다. "재시작해야 한다" 같은 처방은 쓰지 마라.
- 정상적인 운영 소음은 보고하지 마라: 외부 API의 429/타임아웃/연결 끊김, 재시도 후 성공, 사용자가 취소한 턴.
- **반복**이 가장 강한 신호다. ×N 표시가 큰 항목을 우선 본다.
- 확신이 없으면 severity를 low로 낮춰라. 기각당할 발견을 high로 올리면 원장 전체가 무시된다.
- 이상이 없으면 findings를 빈 배열로 두어라. 억지로 만들지 마라.

출력은 JSON만. 다른 텍스트 금지:
{"findings":[{"severity":"low|medium|high","summary":"무엇이 이상한가 (한 문장)","evidence":"인용한 로그 줄 그대로","whyItMatters":"이게 왜 문제인가 (한 문장)"}]}`

// Judge is the model call, injected so the lane is testable without a model and
// so the role resolution stays with the caller (pilot.CallLocalLLM → the
// lightweight role, which is the local dsv4).
type Judge func(ctx context.Context, system, user string, maxTokens int) (string, error)

// maxJudgeTokens bounds the reply. Findings are one-liners with a quote; a
// larger budget buys only a longer essay about the same window.
const maxJudgeTokens = 1500

// Inspect runs one pass over the digest and returns the findings it can stand
// behind, plus a gap string when it could not reach a verdict at all.
//
// The gap return is separate from an error on purpose: a model being
// unreachable is a fact about the pass that belongs IN the ledger, not an
// error that aborts the write and leaves the ledger silent about the outage.
func Inspect(ctx context.Context, judge Judge, d Digest) (findings []Finding, gap string) {
	if judge == nil {
		return nil, "판정 모델이 배선되지 않음"
	}
	if strings.TrimSpace(d.Text) == "" {
		return nil, "읽을 로그 창이 비어 있음 (링 미배선 의심)"
	}
	reply, err := judge(ctx, systemPrompt, "로그 창:\n"+d.Text, maxJudgeTokens)
	if err != nil {
		return nil, fmt.Sprintf("판정 호출 실패: %v", err)
	}
	parsed, err := parseFindings(reply)
	if err != nil {
		return nil, fmt.Sprintf("판정 응답 해석 실패: %v", err)
	}
	return keepGrounded(parsed, d.Text), ""
}

// parseFindings tolerates a model that wraps its JSON in prose or a fence,
// which every local model does occasionally regardless of instruction.
func parseFindings(reply string) ([]Finding, error) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 && j < len(s)-1 {
		s = s[:j+1]
	}
	var out struct {
		Findings []Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out.Findings, nil
}

// keepGrounded drops findings whose evidence is not actually in the window.
//
// This is the lane's one hard gate, and it exists because the alternative was
// measured: log-derived claims that sound right and are not are the dominant
// output of this kind of reading. A quote that cannot be found in the window is
// a quote the model composed, and a composed quote costs the reader exactly the
// investigation the ledger was supposed to save.
func keepGrounded(findings []Finding, window string) []Finding {
	var kept []Finding
	for _, f := range findings {
		f.Summary = strings.TrimSpace(f.Summary)
		f.Evidence = strings.TrimSpace(f.Evidence)
		f.WhyItMatters = strings.TrimSpace(f.WhyItMatters)
		if f.Summary == "" || f.Evidence == "" {
			continue
		}
		if !quoteAppearsIn(f.Evidence, window) {
			continue
		}
		f.Severity = normalizeSeverity(f.Severity)
		kept = append(kept, f)
	}
	return kept
}

// quoteAppearsIn checks the quote against the window, allowing for the model
// trimming a line rather than copying it whole. The distinctive head of the
// line is enough to locate it; requiring a byte-exact copy would reject honest
// findings for cosmetic reasons.
func quoteAppearsIn(quote, window string) bool {
	q := strings.TrimSpace(quote)
	if q == "" {
		return false
	}
	if strings.Contains(window, q) {
		return true
	}
	r := []rune(q)
	if len(r) > 40 {
		return strings.Contains(window, string(r[:40]))
	}
	return false
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "critical":
		return "high"
	case "medium", "moderate":
		return "medium"
	default:
		// Unknown reads DOWN, not up: an unlabeled finding is not an urgent one.
		return "low"
	}
}
