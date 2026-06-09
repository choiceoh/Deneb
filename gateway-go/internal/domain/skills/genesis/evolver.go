package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*EvolutionTask)(nil)

// DefaultEvolveEventThreshold is how many new skills must accumulate before the
// event-driven evolve trigger fires (every 5 new skills), instead of waiting
// for the 6h periodic cycle.
const DefaultEvolveEventThreshold = 5

// EvolveResult describes the outcome of an evolution attempt.
type EvolveResult struct {
	SkillName   string `json:"skillName"`
	Evolved     bool   `json:"evolved"`
	NewVersion  string `json:"newVersion,omitempty"`
	Description string `json:"description,omitempty"`
	Reason      string `json:"reason,omitempty"` // when skipped
}

// Evolver auto-improves skills based on usage data.
type Evolver struct {
	llmClient *llm.Client
	catalog   *skills.Catalog
	tracker   *Tracker
	model     string
	logger    *slog.Logger

	// selfTest gates the verification loop: when true, a rewritten skill is
	// judged before being committed (a bad "improvement" is worse than none).
	selfTest bool
	// teacherClient/teacherModel are an optional stronger (main) model used to
	// judge and re-attempt when the lightweight rewrite fails self-test (#4
	// teacher-escalation). nil → no escalation.
	teacherClient *llm.Client
	teacherModel  string

	// runMu serializes evolve cycles so the periodic task and the event
	// trigger can't overlap (TryLock: a second concurrent caller skips).
	runMu sync.Mutex
}

// NewEvolver creates a skill evolver. Self-test defaults on; disable with
// DENEB_SKILL_EVOLVE_SELFTEST=0.
func NewEvolver(llmClient *llm.Client, catalog *skills.Catalog, tracker *Tracker, model string, logger *slog.Logger) *Evolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Evolver{
		llmClient: llmClient,
		catalog:   catalog,
		tracker:   tracker,
		model:     model,
		logger:    logger,
		selfTest:  envBool("DENEB_SKILL_EVOLVE_SELFTEST", true),
	}
}

// SetTeacher wires an optional stronger model (typically modelrole main) used
// to escalate a rewrite that fails the lightweight self-test. Safe to call
// with a nil client (no-op escalation).
func (e *Evolver) SetTeacher(client *llm.Client, model string) {
	e.teacherClient = client
	e.teacherModel = model
}

// EvolveSkill attempts to improve a single skill based on usage feedback.
// EvolveSkill improves one skill. reviewFinding is an optional improvement
// directive from a background skill-review (the LLM that just observed a
// session); when present it is the primary basis for the rewrite and lets the
// evolver proceed even with no usage data — usage-stat-driven evolution
// otherwise never fires because skill usage is sparsely recorded.
func (e *Evolver) EvolveSkill(ctx context.Context, skillName, reviewFinding string) (*EvolveResult, error) {
	if e.catalog == nil {
		return nil, fmt.Errorf("evolver: catalog not configured")
	}
	// Get current skill content.
	entry, ok := e.catalog.Get(skillName)
	if !ok {
		return nil, fmt.Errorf("evolver: skill %q not found in catalog", skillName)
	}

	currentContent, err := os.ReadFile(entry.Skill.FilePath)
	if err != nil {
		return nil, fmt.Errorf("evolver: read skill file: %w", err)
	}

	// Get usage stats.
	var stats *UsageStats
	if e.tracker != nil {
		stats, _ = e.tracker.Stats(skillName)
	}
	if stats == nil {
		stats = &UsageStats{SkillName: skillName}
	}

	// Build prompt. A review-provided finding (when present) is the primary
	// basis for improvement and lets the evolver proceed without usage data.
	findingSection := ""
	if strings.TrimSpace(reviewFinding) != "" {
		findingSection = "\n\n## Review Finding (개선 지시 — 우선 반영)\n" + strings.TrimSpace(reviewFinding)
	}
	userPrompt := fmt.Sprintf(`## 현재 SKILL.md
%s

## 사용 통계
- 총 사용: %d회
- 성공: %d회 (%.0f%%)
- 실패: %d회
- 최근 에러: %s%s`,
		string(currentContent),
		stats.TotalUses, stats.SuccessCount, stats.SuccessRate*100,
		stats.FailureCount,
		formatRecentErrors(stats.RecentErrors),
		findingSection)

	events, err := e.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          e.resolveModel(),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(evolveSystemPrompt),
		MaxTokens:      2048,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("evolver LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("evolver LLM: nil event channel")
	}

	var sb strings.Builder
	for ev := range events {
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}

	return e.parseAndApply(ctx, sb.String(), entry, string(currentContent), stats)
}

// EvolveUnderperformers finds and evolves skills with poor success rates.
// Used as a periodic background task.
func (e *Evolver) EvolveUnderperformers(ctx context.Context) ([]EvolveResult, error) {
	if e.tracker == nil {
		return nil, nil
	}
	// Serialize cycles: the 6h periodic task and the event trigger both call
	// this; if one is already running, the other skips rather than double-work.
	if !e.runMu.TryLock() {
		return nil, nil
	}
	defer e.runMu.Unlock()

	candidates, err := e.tracker.SkillsNeedingEvolution(3, 0.7)
	if err != nil {
		return nil, err
	}

	var results []EvolveResult
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		result, err := e.EvolveSkill(ctx, candidate.SkillName, "")
		if err != nil {
			e.logger.Warn("evolver: failed to evolve",
				"skill", candidate.SkillName, "error", err)
			results = append(results, EvolveResult{
				SkillName: candidate.SkillName,
				Evolved:   false,
				Reason:    err.Error(),
			})
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, nil //nolint:nilerr // individual skill errors collected in results, not propagated
}

func (e *Evolver) parseAndApply(ctx context.Context, text string, entry *skills.SkillEntry, originalContent string, stats *UsageStats) (*EvolveResult, error) {
	extracted := jsonutil.ExtractObject(text)
	if extracted == "" {
		extracted = strings.TrimSpace(text)
	}

	var resp struct {
		Skip    bool   `json:"skip"`
		Reason  string `json:"reason,omitempty"`
		Changes *struct {
			Description string `json:"description"`
			NewVersion  string `json:"new_version"`
			Body        string `json:"body"`
		} `json:"changes,omitempty"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err != nil {
		return nil, fmt.Errorf("evolver: parse response: %w", err)
	}

	if resp.Skip || resp.Changes == nil {
		return &EvolveResult{
			SkillName: entry.Skill.Name,
			Evolved:   false,
			Reason:    resp.Reason,
		}, nil
	}

	// Reconstruct SKILL.md with updated body.
	header, bodyOffset := skills.ExtractFrontmatterBlock(originalContent)
	if bodyOffset == 0 || header == "" {
		return nil, fmt.Errorf("evolver: skill %q has no valid frontmatter", entry.Skill.Name)
	}

	// Update version in frontmatter.
	newVersion := resp.Changes.NewVersion
	if newVersion == "" {
		newVersion = bumpPatchVersion(entry.Skill.Version)
	}

	candidateBody := resp.Changes.Body

	// Self-test the rewrite before committing it. A failed or uncertain judge
	// keeps the original — a bad "improvement" is worse than no change. When a
	// teacher (main) model is wired, it gets one escalated attempt first (#4).
	if e.selfTest {
		body, ok, reason := e.selfTestAndMaybeEscalate(ctx, entry, originalContent, candidateBody, stats)
		if !ok {
			return &EvolveResult{
				SkillName: entry.Skill.Name,
				Evolved:   false,
				Reason:    "self-test rejected: " + reason,
			}, nil
		}
		candidateBody = body
	}

	newHeader := strings.Replace(header, entry.Skill.Version, newVersion, 1)
	newContent := newHeader + "\n" + candidateBody + "\n"

	// Write back atomically so a crash mid-write can't corrupt the skill.
	if err := atomicfile.WriteFile(entry.Skill.FilePath, []byte(newContent), &atomicfile.Options{Perm: 0o644}); err != nil {
		return nil, fmt.Errorf("evolver: write file: %w", err)
	}

	e.logger.Info("evolver: skill evolved",
		"skill", entry.Skill.Name,
		"version", newVersion,
		"description", resp.Changes.Description,
	)

	return &EvolveResult{
		SkillName:   entry.Skill.Name,
		Evolved:     true,
		NewVersion:  newVersion,
		Description: resp.Changes.Description,
	}, nil
}

func (e *Evolver) resolveModel() string {
	if e.model != "" {
		return e.model
	}
	return "gemini-2.5-flash"
}

// bumpPatchVersion increments the patch segment of a semver string.
func bumpPatchVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "0.1.1"
	}
	var patch int
	fmt.Sscanf(parts[2], "%d", &patch) //nolint:errcheck // partial parse ok
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

func formatRecentErrors(errors []string) string {
	if len(errors) == 0 {
		return "(none)"
	}
	var parts []string
	for _, e := range errors {
		if len(e) > 100 {
			e = e[:100] + "..."
		}
		parts = append(parts, "- "+e)
	}
	return strings.Join(parts, "\n")
}

// drainStreamText collects the assistant text from a streamed completion.
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

// selfTestAndMaybeEscalate judges a candidate rewrite. On pass it returns the
// candidate. On fail it escalates to the teacher model (if wired) for one more
// attempt, then re-judges. Returns (finalBody, ok, reason). ok=false means the
// caller must keep the original skill untouched.
func (e *Evolver) selfTestAndMaybeEscalate(ctx context.Context, entry *skills.SkillEntry, originalContent, candidateBody string, stats *UsageStats) (body string, ok bool, reason string) {
	pass, reason, err := e.judgeCandidate(ctx, e.llmClient, e.resolveModel(), originalContent, candidateBody, stats)
	if err != nil {
		e.logger.Warn("evolver: self-test errored, keeping original",
			"skill", entry.Skill.Name, "error", err)
		return "", false, "judge error"
	}
	if pass {
		return candidateBody, true, reason
	}
	e.logger.Info("evolver: self-test rejected lightweight rewrite",
		"skill", entry.Skill.Name, "reason", reason)

	// Teacher-escalation: let the stronger model rewrite once, then re-judge
	// with the teacher so the bar is held by the model best able to meet it.
	if e.teacherClient == nil || e.teacherModel == "" {
		return "", false, reason
	}
	teacherBody, terr := e.teacherRewrite(ctx, originalContent, candidateBody, reason, stats)
	if terr != nil || strings.TrimSpace(teacherBody) == "" {
		e.logger.Warn("evolver: teacher escalation failed",
			"skill", entry.Skill.Name, "error", terr)
		return "", false, "teacher escalation failed"
	}
	tpass, treason, tjerr := e.judgeCandidate(ctx, e.teacherClient, e.teacherModel, originalContent, teacherBody, stats)
	if tjerr != nil || !tpass {
		e.logger.Info("evolver: teacher rewrite still failed self-test",
			"skill", entry.Skill.Name, "reason", treason)
		return "", false, "teacher: " + treason
	}
	e.logger.Info("evolver: teacher escalation succeeded", "skill", entry.Skill.Name)
	return teacherBody, true, treason
}

// judgeCandidate asks a model to validate a rewritten skill body against the
// original. Returns (pass, reason, err). On any error the caller keeps the
// original (fail-closed).
func (e *Evolver) judgeCandidate(ctx context.Context, client *llm.Client, model, originalContent, candidateBody string, stats *UsageStats) (pass bool, reason string, err error) {
	if client == nil {
		return false, "", fmt.Errorf("judge: nil client")
	}
	userPrompt := fmt.Sprintf(`## 원본 SKILL.md
%s

## 개선된 본문 (검증 대상)
%s

## 사용 이력
- 총 %d회, 성공 %d, 실패 %d (%.0f%%)
- 최근 에러: %s`,
		originalContent, candidateBody,
		stats.TotalUses, stats.SuccessCount, stats.FailureCount, stats.SuccessRate*100,
		formatRecentErrors(stats.RecentErrors))

	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(skillJudgeSystemPrompt),
		MaxTokens:      512,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return false, "", fmt.Errorf("judge LLM call: %w", err)
	}
	if events == nil {
		return false, "", fmt.Errorf("judge: nil event channel")
	}
	extracted := jsonutil.ExtractObject(drainStreamText(events))
	if extracted == "" {
		return false, "", fmt.Errorf("judge: empty verdict")
	}
	var resp struct {
		Pass   bool   `json:"pass"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err != nil {
		return false, "", fmt.Errorf("judge: parse verdict: %w", err)
	}
	return resp.Pass, resp.Reason, nil
}

// teacherRewrite asks the stronger model to produce a better body after the
// lightweight rewrite failed self-test. Reuses the evolve envelope; returns
// the new body (or "" when the teacher declines).
func (e *Evolver) teacherRewrite(ctx context.Context, originalContent, failedCandidate, rejectReason string, stats *UsageStats) (string, error) {
	userPrompt := fmt.Sprintf(`## 현재 SKILL.md
%s

## 직전 개선 시도 (검증 실패)
%s

## 검증 실패 사유
%s

## 사용 통계
- 총 %d회, 성공 %d, 실패 %d (%.0f%%)
- 최근 에러: %s

위 실패 사유를 해결한 개선된 SKILL.md body 를 생성하세요. 검증 기준(명확성·실재 도구만·구조 유지·범주 수준·실패패턴 해결)을 모두 만족해야 합니다.`,
		originalContent, failedCandidate, rejectReason,
		stats.TotalUses, stats.SuccessCount, stats.FailureCount, stats.SuccessRate*100,
		formatRecentErrors(stats.RecentErrors))

	events, err := e.teacherClient.StreamChat(ctx, llm.ChatRequest{
		Model:          e.teacherModel,
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(evolveSystemPrompt),
		MaxTokens:      2048,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", fmt.Errorf("teacher rewrite LLM call: %w", err)
	}
	if events == nil {
		return "", fmt.Errorf("teacher rewrite: nil event channel")
	}
	extracted := jsonutil.ExtractObject(drainStreamText(events))
	if extracted == "" {
		return "", nil
	}
	var resp struct {
		Skip    bool `json:"skip"`
		Changes *struct {
			Body string `json:"body"`
		} `json:"changes,omitempty"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err != nil {
		return "", fmt.Errorf("teacher rewrite: parse: %w", err)
	}
	if resp.Skip || resp.Changes == nil {
		return "", nil
	}
	return resp.Changes.Body, nil
}

// EvolutionTask implements autonomous.PeriodicTask for background skill evolution.
type EvolutionTask struct {
	Evolver *Evolver
	Logger  *slog.Logger
}

// Name returns the task identifier.
func (t *EvolutionTask) Name() string { return "skill-evolution" }

// Interval returns how often to check for underperforming skills.
func (t *EvolutionTask) Interval() time.Duration { return 6 * time.Hour }

// Run executes one evolution cycle.
func (t *EvolutionTask) Run(ctx context.Context) error {
	results, err := t.Evolver.EvolveUnderperformers(ctx)
	// Heartbeat: records that the evolve cycle actually ran (liveness on /health).
	if t.Evolver != nil && t.Evolver.tracker != nil {
		t.Evolver.tracker.RecordEvolutionActivity(SkillActivityEvolve, err == nil, errString(err))
	}
	if err != nil {
		return err
	}
	evolved := 0
	for _, r := range results {
		if r.Evolved {
			evolved++
		}
	}
	if evolved > 0 {
		t.Logger.Info("skill-evolution: cycle complete",
			"evolved", evolved, "total", len(results))
	}
	return nil
}
