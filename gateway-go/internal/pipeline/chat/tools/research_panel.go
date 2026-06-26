// research_panel.go — the research_panel agent tool. Fans one question out to
// every healthy model the wormhole router serves, in parallel, and returns each
// model's answer labeled by model + family. The agent (deep-research skill) is
// the aggregator: it synthesizes the labeled candidates itself, weighting
// cross-family agreement over within-family (echo chamber) and discounting weak
// proposers (Self-MoA 2502.00674) — this tool only collects.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// ConsultPanelFn is the server-injected fan-out engine (CoreToolDeps.ConsultPanel):
// (system, prompt, models) → one answer per model. Empty models = all healthy.
type ConsultPanelFn func(ctx context.Context, system, prompt string, models []string) []toolctx.PanelAnswer

// panelistSystem instructs each panelist to give a substantive, honest, Korean
// answer. Kept short + identical for every model so the only variable is the
// model itself (the comparison the synthesizer relies on).
const panelistSystem = "당신은 리서치 패널의 한 패널리스트입니다. 주어진 질문에 아는 한도에서 충실하고 구체적으로 답하되, 모르거나 불확실하면 그렇다고 명시하세요. 핵심 근거를 담아 간결하게, 한국어로 답하세요."

// ToolResearchPanel returns the research_panel tool. consult is the server's
// fan-out engine; a nil consult is rejected at registration so the Fn never
// sees one, but it is guarded here too for safety.
func ToolResearchPanel(consult ConsultPanelFn) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if consult == nil {
			return "", fmt.Errorf("research panel unavailable: model router not wired")
		}
		var p struct {
			Question string   `json:"question"`
			Models   []string `json:"models"`
		}
		if len(input) > 0 {
			if err := json.Unmarshal(input, &p); err != nil {
				return "", fmt.Errorf("invalid research_panel params: %w", err)
			}
		}
		question := strings.TrimSpace(p.Question)
		if question == "" {
			return "", fmt.Errorf("research_panel: question is required")
		}

		answers := consult(ctx, panelistSystem, question, p.Models)
		if len(answers) == 0 {
			return "리서치 패널에 응답한 모델이 없습니다 (현재 헬시 모델 없음 또는 모델 라우터 미가동). 라우터 상태를 확인하거나, 이번 질문은 단일 모델로 답하세요.", nil
		}
		return formatPanelAnswers(question, answers), nil
	}
}

// formatPanelAnswers renders the panel for the agent-as-aggregator: a synthesis
// directive up top, then each successful answer labeled by model + family +
// latency, with failures listed compactly at the end.
func formatPanelAnswers(question string, answers []toolctx.PanelAnswer) string {
	// Successes first (stable by model name); failures sink to the bottom.
	sort.SliceStable(answers, func(i, j int) bool {
		oi, oj := answers[i].Err == "", answers[j].Err == ""
		if oi != oj {
			return oi
		}
		return answers[i].Model < answers[j].Model
	})

	ok := 0
	families := map[string]struct{}{}
	for _, a := range answers {
		if a.Err == "" {
			ok++
			families[a.Family] = struct{}{}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "리서치 패널 — 모델 %d개 호출, %d개 응답(%d개 계열). 질문: %s\n\n",
		len(answers), ok, len(families), question)
	b.WriteString("종합 지침(당신이 종합자다): 각 답에 모델명·계열 라벨이 붙어 있다. " +
		"**서로 다른 계열이 합의한 점일수록 더 신뢰**하고(같은 계열끼리의 합의는 같은 맹점일 수 있다), " +
		"모순은 어느 모델이 어느 쪽인지 밝혀 직접 판정하라 — 가장 자신만만한 답에 닻 내리지 말 것. " +
		"소형·약한 모델의 답은 참고로만 가중하라. 어느 답으로도 검증되지 않은 주장은 '미확인'으로 표시하라. " +
		"**모든 패널리스트의 답을 빠짐없이 검토하라** — 어떤 모델의 기여도 말없이 누락하지 말고, " +
		"한 답을 배제한다면 그 이유를 한 줄로 밝혀라(다중 패널 종합의 가장 흔한 실패는 일부 답을 통째로 무시하는 것이다).\n")

	idx := 0
	for _, a := range answers {
		if a.Err != "" {
			continue
		}
		idx++
		fmt.Fprintf(&b, "\n[%d] model=%s · family=%s · %dms\n%s\n", idx, a.Model, a.Family, a.Ms, a.Answer)
	}

	var dropped []string
	for _, a := range answers {
		if a.Err != "" {
			dropped = append(dropped, a.Model)
		}
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&b, "\n(응답 실패·시간초과로 제외된 모델: %s)\n", strings.Join(dropped, ", "))
	}
	return b.String()
}
