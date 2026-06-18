// Package compactuner is the ACON-style background loop that refines the
// compaction summarizer's preservation guidelines from observed weakness.
//
// Each cycle it audits recent compaction summaries for "vagueness" — a category
// mentioned but its concrete value dropped (an amount written as "비용 논의"
// with no number, a person as "담당자" with no name). The dominant gaps become
// one-line preservation guidelines, merged into the GuidelineStore (additive,
// deduped, capped) that the summarizer prompt reads. This needs no re-runs or
// trajectory pairs: vagueness is detectable from the summary text alone, and
// the guidelines only ever ADD "preserve X", never relax the hardcoded rules.
//
// Opt-in (it auto-edits a prompt): registered only when DENEB_COMPACTION_TUNER
// is set. The operator is notified on every change and can clear the store file.
package compactuner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	taskInterval = 24 * time.Hour
	// minSummaries is the floor for auditing — too few and a single odd summary
	// skews the proposal.
	minSummaries = 4
	auditLimit   = 12 // newest summaries to audit per cycle
	maxSummaryLn = 1500
	llmTimeout   = 60 * time.Second
	llmMaxTokens = 400
)

// SummarySource yields recent compaction summaries (polaris store).
type SummarySource interface {
	RecentSummariesAcrossSessions(limit int) []polaris.SummaryNode
}

// llmClient is the slice of *llm.Client the tuner needs (mockable in tests).
type llmClient interface {
	StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error)
}

// Deps wires the compaction tuner.
type Deps struct {
	Summaries  SummarySource
	Guidelines *compaction.GuidelineStore
	Client     llmClient
	Model      string
	Notify     func(ctx context.Context, msg string) error // optional operator delivery
	Logger     *slog.Logger
}

// Task is the autonomous.PeriodicTask.
type Task struct {
	deps Deps
	mu   sync.Mutex
}

// Report describes one tuner cycle. It is intentionally small and wire-safe so
// operator surfaces can show why an automatic prompt-improvement pass changed
// something, or why it was a no-op.
type Report struct {
	Ran           bool     `json:"ran"`
	Changed       bool     `json:"changed"`
	Reason        string   `json:"reason"`
	Error         string   `json:"error,omitempty"`
	LeafSummaries int      `json:"leafSummaries"`
	MinSummaries  int      `json:"minSummaries"`
	Proposed      []string `json:"proposed,omitempty"`
	Added         []string `json:"added,omitempty"`
	BeforeCount   int      `json:"beforeCount"`
	AfterCount    int      `json:"afterCount"`
}

// NewTask builds the tuner task.
func NewTask(d Deps) *Task {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Task{deps: d}
}

func (t *Task) Name() string            { return "compaction-tuner" }
func (t *Task) Interval() time.Duration { return taskInterval }

// Run audits recent summaries and merges any new preservation guidelines.
func (t *Task) Run(ctx context.Context) error {
	report := t.RunWithReport(ctx)
	switch report.Reason {
	case "updated":
		t.deps.Logger.Info("compaction-tuner: guidelines updated",
			"added", report.Added, "total", report.AfterCount)
	case "critique_failed", "save_failed":
		t.deps.Logger.Warn("compaction-tuner: cycle failed",
			"reason", report.Reason, "error", report.Error)
	default:
		t.deps.Logger.Debug("compaction-tuner: no change",
			"reason", report.Reason, "leafSummaries", report.LeafSummaries)
	}
	if report.Changed && t.deps.Notify != nil {
		after := t.deps.Guidelines.Load()
		msg := "압축 요약 보존 지침이 갱신되었습니다 (과거 요약의 구체성 부족에서 학습):\n- " + strings.Join(after, "\n- ")
		if err := t.deps.Notify(ctx, msg); err != nil {
			t.deps.Logger.Warn("compaction-tuner: notify failed", "error", err)
		}
	}
	return nil
}

// RunWithReport audits recent summaries and returns a structured outcome.
// It preserves Run's safety posture: recoverable model/save failures are
// reported as no-change outcomes rather than making the periodic loop fail.
func (t *Task) RunWithReport(ctx context.Context) Report {
	t.mu.Lock()
	defer t.mu.Unlock()

	report := Report{MinSummaries: minSummaries}
	if t.deps.Summaries == nil || t.deps.Guidelines == nil || t.deps.Client == nil {
		report.Reason = "unavailable"
		return report
	}
	texts := leafSummaryTexts(t.deps.Summaries.RecentSummariesAcrossSessions(auditLimit))
	report.LeafSummaries = len(texts)
	if len(texts) < minSummaries {
		report.Reason = "too_few_summaries"
		return report
	}

	bullets, err := t.critique(ctx, texts)
	if err != nil {
		report.Ran = true
		report.Reason = "critique_failed"
		report.Error = err.Error()
		return report
	}
	report.Ran = true
	report.Proposed = append([]string{}, bullets...)
	if len(bullets) == 0 {
		report.Reason = "no_guidelines"
		return report
	}

	before := t.deps.Guidelines.Load()
	report.BeforeCount = len(before)
	// Newest-first: prepend this cycle's proposals; Save dedups + caps.
	if err := t.deps.Guidelines.Save(append(append([]string{}, bullets...), before...)); err != nil {
		report.Reason = "save_failed"
		report.Error = err.Error()
		return report
	}
	after := t.deps.Guidelines.Load()
	report.AfterCount = len(after)
	if strings.Join(after, "\n") == strings.Join(before, "\n") {
		report.Reason = "duplicate_guidelines"
		return report
	}

	report.Changed = true
	report.Reason = "updated"
	report.Added = addedGuidelines(before, after)
	return report
}

// leafSummaryTexts keeps non-empty level-1 (leaf) summary bodies — condensed
// (level 2+) nodes are summaries-of-summaries and less useful for spotting
// concrete-value loss.
func leafSummaryTexts(nodes []polaris.SummaryNode) []string {
	var out []string
	for _, n := range nodes {
		if n.Level == 1 {
			if c := strings.TrimSpace(n.Content); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

func addedGuidelines(before, after []string) []string {
	seen := make(map[string]bool, len(before))
	for _, g := range before {
		seen[g] = true
	}
	out := make([]string, 0, len(after))
	for _, g := range after {
		if !seen[g] {
			out = append(out, g)
		}
	}
	return out
}

const auditSystemPrompt = `당신은 대화 자동요약의 품질 감사자다. 아래 요약들에서 "구체성 부족" 패턴을 찾아라:
범주는 언급하지만 실제 값이 빠진 경우다. 예시:
- 금액을 "비용 논의"로만 적고 정확한 숫자/통화 누락
- 사람을 "담당자"로만 적고 이름 누락
- 날짜를 "다음 주"로만 적고 정확한 날짜 누락
- 결정을 "방향 정함"으로만 적고 무엇으로 정했는지 누락

가장 흔하게 빠지는 구체 정보를 최대 2개, 요약기에게 줄 한 줄 보존 지침으로 작성하라.
각 지침은 한국어 한 줄, 60자 이내, "~을(를) 보존하라" 형태.
이미 충분히 구체적이면 빈 배열을 반환하라.

출력은 JSON만: {"guidelines": ["금액은 정확한 숫자와 통화로 보존하라", "..."]}`

// critique asks the LLM to surface the dominant vagueness patterns as
// guidelines. Returns nil when the model judges the summaries specific enough.
func (t *Task) critique(ctx context.Context, summaries []string) ([]string, error) {
	var sb strings.Builder
	for i, s := range summaries {
		fmt.Fprintf(&sb, "### 요약 %d\n%s\n\n", i+1, clip(s, maxSummaryLn))
	}

	cctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()
	events, err := t.deps.Client.StreamChat(cctx, llm.ChatRequest{
		Model:          t.deps.Model,
		Messages:       []llm.Message{llm.NewTextMessage("user", "다음은 대화 자동요약들이다.\n\n"+sb.String())},
		System:         llm.SystemString(auditSystemPrompt),
		MaxTokens:      llmMaxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("compaction-tuner: nil event channel")
	}
	return parseGuidelines(drainStreamText(events)), nil
}

// parseGuidelines extracts the guidelines array from the model's JSON verdict.
// Tolerant of surrounding prose: pulls the JSON object first.
func parseGuidelines(text string) []string {
	extracted := jsonutil.ExtractObject(text)
	if extracted == "" {
		return nil
	}
	var resp struct {
		Guidelines []string `json:"guidelines"`
	}
	if json.Unmarshal([]byte(extracted), &resp) != nil {
		return nil
	}
	out := make([]string, 0, len(resp.Guidelines))
	for _, g := range resp.Guidelines {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func drainStreamText(events <-chan llm.StreamEvent) string {
	var sb strings.Builder
	for ev := range events {
		if ev.Type != "content_block_delta" {
			continue
		}
		var delta struct {
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
			sb.WriteString(delta.Delta.Text)
		}
	}
	return sb.String()
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
