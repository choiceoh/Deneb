// pipeline_batch.go — morning batch analysis: one synthesized report over
// N emails (AnalyzeBatch), with per-item context built by the same stages as
// the single-email pipeline (pipeline.go).
package gmailpoll

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/hanja"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

const batchAnalysisSystem = `당신은 비서 역할의 이메일 분석 어시스턴트입니다.
여러 이메일을 한꺼번에 읽고, 그 사안들이 요구하는 방식으로 묶어 우선순위가 분명한 통합 리포트를 작성합니다.
` + emojiRestraint

const batchAnalysisPrompt = `%s

다음 %d건의 이메일을 한꺼번에 읽고 하나의 통합
리포트로 묶어주세요. 이건 채워야 할 양식이 아니라 분석 자세입니다.

여러 메일을 가로질러 큰 그림을 보세요 — 오늘 무엇이 가장 급하고 중요한지, 어떤
메일들이 같은 사안으로 묶이는지. 무엇을 앞세우고 어떻게 묶을지(프로젝트별·시간순·
이해관계자별·긴급도순 등)는 그날의 메일들이 정합니다. 고정된 틀에 끼워 맞추지 말고,
가장 중요한 것이 맨 위에 오게 하세요.

읽기 쉽게 돕는 장치는 자유롭게 쓰되 의무는 아닙니다: 그룹 사이 구분선, 발신자 실명,
핵심 불릿, 다음 행동. 사안이 단순하면 간략히, 복잡하면 깊게.

지켜야 할 것: 한국어로 작성하고, 모든 메일을 빠짐없이 포함하며, 금액·날짜·수량 같은
구체적 수치는 그대로 살리세요. 근거 없는 추측으로 메우지 마세요.

%s`

// BatchItem pairs an email with its individual analysis result. AnalyzeBatch
// returns these so the caller can cache/page each one, and the consolidated
// report is synthesized from the originals + these analyses.
type BatchItem struct {
	Msg    *gmail.MessageDetail
	Result AnalysisResult
}

// AnalyzeBatch analyzes a batch of emails: each email is analyzed
// individually (including related-project selection), and those per-email
// results are both returned (for the caller to cache/page) and fed — along
// with the original emails — into a single consolidated report grouped by
// project and priority. Per-email analysis is best-effort: a failed one is
// logged and skipped rather than failing the whole batch.
func AnalyzeBatch(ctx context.Context, deps PipelineDeps, msgs []*gmail.MessageDetail) (string, []BatchItem, error) {
	if len(msgs) == 0 {
		return "", nil, fmt.Errorf("no emails to analyze")
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Phase 1: individual analysis per email. Sequential to bound concurrent
	// LLM load on the local server (the batch caps at MaxPerCycle anyway).
	// These per-email results are the single source of truth — the caller
	// caches/pages each one, and they feed the consolidated report below.
	items := make([]BatchItem, 0, len(msgs))
	for _, msg := range msgs {
		res, err := AnalyzeEmailPipeline(ctx, deps, msg)
		if err != nil {
			logger.Warn("개별 메일 분석 실패 — 건너뜀", "id", msg.ID, "error", err)
			continue
		}
		items = append(items, BatchItem{Msg: msg, Result: res})
	}
	if len(items) == 0 {
		return "", nil, fmt.Errorf("모든 메일 분석 실패 (%d건)", len(msgs)) //nolint:staticcheck // ST1005 — Korean error message
	}

	// Single email: the individual analysis IS the report — no consolidation.
	if len(items) == 1 {
		return items[0].Result.Text, items, nil
	}

	// Phase 2: consolidated report from originals + individual analyses.
	stage2Ctx, cancel := context.WithTimeout(ctx, stage2Timeout)
	defer cancel()
	report, err := synthesizeBatchReport(stage2Ctx, deps, items)
	if err != nil {
		// Per-email results are still valuable (cache/page) even if the
		// consolidated report failed — return them alongside the error.
		return "", items, err
	}
	return report, items, nil
}

// synthesizeBatchReport generates the consolidated report from the original
// emails plus their individual analyses. Feeding both — not just the
// analyses — gives the model the full source material for a richer
// big-picture report (grouping by project, prioritizing across emails).
func synthesizeBatchReport(ctx context.Context, deps PipelineDeps, items []BatchItem) (string, error) {
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&sb, "### 메일 %d\n", i+1)
		sb.WriteString(FormatEmailForAnalysis(it.Msg))
		if analysis := strings.TrimSpace(it.Result.Text); analysis != "" {
			sb.WriteString("\n\n[개별 분석]\n")
			sb.WriteString(analysis)
			sb.WriteString("\n")
		}
	}

	userPrompt := fmt.Sprintf(batchAnalysisPrompt, analysisPrompt(deps), len(items), sb.String())

	// Reasoning OFF. GLM-5.1 (Z.ai anthropic endpoint) defaults reasoning ON and
	// streams its chain-of-thought into the answer body as ordinary text, which
	// collectStreamText can't tell apart from real content. Sending
	// {"type":"disabled"} (see anthropic.go) turns it off at the source;
	// stripReasoningLeak below still scrubs any stray marker.
	req := llm.ChatRequest{
		Model:     deps.MainModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:    llm.SystemString(batchAnalysisSystem),
		MaxTokens: batchStage2Tokens,
		Stream:    true,
		Thinking:  &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: deps.ThinkingKwarg},
	}

	events, err := deps.LLMClient.StreamChat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("batch analysis LLM call failed: %w", err)
	}

	report, err := collectStreamText(ctx, events)
	if err != nil {
		return "", err
	}
	// Read Sino-Korean Hanja in the consolidated report as Hangul (the analysis
	// model may be a Chinese-lineage one). Per-email items were transliterated in
	// synthesizeAnalysis; this covers the cross-email synthesis prose.
	return hanja.Transliterate(sanitizeAnalysisLeak(report)), nil
}

// extractThreadContext fetches related emails and extracts thread context via local LLM.
