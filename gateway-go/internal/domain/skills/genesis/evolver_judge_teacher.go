package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Judge selection/verdict and teacher rewrite split out of evolver.go (pure
// move, no behavior change).

// pickCandidateJudge returns the client/model that should judge a
// producer-generated candidate. It prefers an independent judge (SetJudge,
// typically modelrole main), then the teacher, and never the producing model
// itself — same-family self-preference bias skews a self-judge toward accepting
// (arXiv:2508.02994). When a dedicated coding model owns rewrites the teacher is
// nil, so without the explicit judge the old logic silently self-judged; the
// judge wire closes that. Falls back to self-judge only when nothing else is
// wired, logging the degraded mode so the misconfig is observable.
func (e *Evolver) pickCandidateJudge() (*llm.Client, string) {
	_, producer := e.primaryModel()
	if client, model := e.judgeModelSnapshot(); client != nil && model != "" && model != producer {
		return client, model
	}
	if client, model := e.teacherModelSnapshot(); client != nil && model != "" && model != producer {
		return client, model
	}
	if e.logger != nil {
		e.logger.Warn("evolver: no independent judge wired, candidate is self-judged (self-preference bias risk)",
			"producer", producer)
	}
	return e.primaryModel()
}

// judgeCandidate asks a model to validate a rewritten skill body against the
// original. Returns (pass, reason, err). On any error the caller keeps the
// original (fail-closed). An accepting forward verdict must additionally
// survive the order-swap consistency probe: the same judge re-grades the pair
// with the bodies in swapped slots and must REJECT that reversed pair
// (RSI P3 — pairwise contrastive judging arXiv:2607.14408; both-orders
// agreement protocol arXiv:2607.12790). A judge that blesses both directions
// graded the slot position, not the content — fail closed.
func (e *Evolver) judgeCandidate(ctx context.Context, skillName string, client *llm.Client, model, originalContent, candidateBody string, stats *UsageStats, prov *evolveProvenance) (pass bool, reason string, err error) {
	if client == nil {
		return false, "", fmt.Errorf("judge: nil client")
	}
	cases := e.validationCasesForPrompt(skillName)
	validationSection := formatValidationCasesForPrompt(cases)
	failurePatternSection := formatFailurePatternsForPrompt(stats)
	covered := len(cases) > 0

	// Stamp the judge identity at the point of judging — captured here, not
	// re-read after, and last-verdict-wins. On teacher escalation the committed
	// body is re-judged by the primary model, so this correctly records the
	// primary as the committed verdict's judge (not the teacher of the first,
	// rejected verdict), keeping judge != producer honest in the record.
	if prov != nil {
		prov.JudgeModel = model
		prov.JudgeArtifactVersion = e.metaVersion(generation.MetaSkillJudgeSystemPrompt)
	}

	resp, err := e.judgeVerdictOnce(ctx, client, model, originalContent, candidateBody, stats, failurePatternSection, validationSection)
	if err != nil {
		return false, "", err
	}
	if prov != nil {
		prov.JudgeScoreOriginal = resp.OriginalScore
		prov.JudgeScoreCandidate = resp.CandidateScore
	}
	pass, reason = acceptJudgeVerdict(resp, covered)
	if pass && !covered {
		// No held-out validation cases cover this skill, so the held-out gate
		// failed open and the judge verdict is the only behavioral check. Require a
		// larger score margin before accepting such a blind evolve (#5). Scores are
		// guaranteed non-nil here because acceptJudgeVerdict only passes with both
		// present.
		if resp.OriginalScore != nil && resp.CandidateScore != nil &&
			*resp.CandidateScore-*resp.OriginalScore < skillUncoveredJudgeMinScoreDelta {
			return false, fmt.Sprintf("uncovered skill (no validation cases): candidate margin %.1f below the %.1f required without held-out coverage: %s",
				*resp.CandidateScore-*resp.OriginalScore, skillUncoveredJudgeMinScoreDelta, reason), nil
		}
	}
	if !pass {
		return false, reason, nil
	}

	if judgeSwapCheckEnabled() {
		swapped, err := e.judgeVerdictOnce(ctx, client, model, candidateBody, originalContent, stats, failurePatternSection, validationSection)
		if err != nil {
			// Fail-closed like every other judge error: without the swapped
			// verdict the both-orders agreement cannot be confirmed.
			return false, "", fmt.Errorf("judge swap probe: %w", err)
		}
		consistent := !judgeSwapInconsistent(swapped, covered)
		if prov != nil {
			prov.JudgeSwapConsistent = &consistent
		}
		if !consistent {
			return false, fmt.Sprintf("judge order-swap inconsistency: reversed pair also accepted (swap scores original=%s candidate=%s) — verdict tracked slot position, not content: %s",
				judgeScoreString(swapped.OriginalScore), judgeScoreString(swapped.CandidateScore), reason), nil
		}
	}
	return true, reason, nil
}

// judgeVerdictOnce runs one strict-improvement judge call with the given
// bodies in the 원본/개선안 prompt slots and parses the verdict. Slot
// assignment is the caller's contract — the order-swap probe deliberately
// reverses it.
func (e *Evolver) judgeVerdictOnce(ctx context.Context, client *llm.Client, model, originalSlot, candidateSlot string, stats *UsageStats, failurePatternSection, validationSection string) (judgeVerdict, error) {
	userPrompt := fmt.Sprintf(`## 원본 SKILL.md
%s

## 개선된 본문 (검증 대상)
%s

## 사용 이력
- 총 %d회, 성공 %d, 실패 %d (%.0f%%)
- 최근 에러: %s%s%s`,
		originalSlot, candidateSlot,
		stats.TotalUses, stats.SuccessCount, stats.FailureCount, stats.SuccessRate*100,
		formatRecentErrors(stats.RecentErrors),
		failurePatternSection,
		validationSection)

	// Non-streaming for the same reason as generateCandidateText: glm-class
	// models leak reasoning into the content stream / append junk when
	// streaming JSON; non-streaming is clean. A provider can still terminate
	// before message_stop or return empty content. Retry the identical,
	// temperature-zero verdict once so a transient transport cut does not throw
	// away a candidate that already passed every deterministic gate. The second
	// failure is returned unchanged, preserving the fail-closed contract.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := client.Complete(ctx, llm.ChatRequest{
			Model:    model,
			Messages: []llm.Message{llm.NewTextMessage("user", userPrompt)},
			System:   llm.SystemString(e.metaLoad(generation.MetaSkillJudgeSystemPrompt, generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])),
			// 4096: verdict JSON is small, but GLM reasoning shares the budget.
			MaxTokens:      4096,
			Temperature:    evolveTemperature(),
			Thinking:       e.thinkingOff(model),
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		})
		if err != nil {
			lastErr = fmt.Errorf("judge LLM call: %w", err)
			continue
		}
		if strings.TrimSpace(raw) == "" {
			lastErr = fmt.Errorf("judge: empty verdict")
			continue
		}
		resp, err := jsonutil.UnmarshalLLM[judgeVerdict](raw)
		if err != nil {
			lastErr = fmt.Errorf("judge: parse verdict: %w", err)
			continue
		}
		return resp, nil
	}
	return judgeVerdict{}, lastErr
}

// judgeSwapCheckEnabled gates the order-swap consistency probe (default on).
// Kill switch: DENEB_JUDGE_SWAP_CHECK=0.
func judgeSwapCheckEnabled() bool {
	return envBool("DENEB_JUDGE_SWAP_CHECK", true)
}

// judgeSwapInconsistent reports whether the swapped-order verdict ALSO cleared
// the strict-improvement rule — the judge blessed both directions of the same
// pair, so its forward accept carries no information about the bodies. The
// same base margin rule as the forward pass applies; the stricter
// uncovered-blind margin stays forward-only (a swap acceptance at the base
// margin is already a contradiction).
func judgeSwapInconsistent(swapped judgeVerdict, covered bool) bool {
	pass, _ := acceptJudgeVerdict(swapped, covered)
	return pass
}

// judgeScoreString formats an optional judge score for reject reasons.
func judgeScoreString(score *float64) string {
	if score == nil {
		return "?"
	}
	return fmt.Sprintf("%.1f", *score)
}

// judgeVerdict is the strict-improvement judge's decision on a candidate skill.
type judgeVerdict struct {
	Pass           bool     `json:"pass"`
	OriginalScore  *float64 `json:"original_score,omitempty"`
	CandidateScore *float64 `json:"candidate_score,omitempty"`
	Reason         string   `json:"reason"`
}

func acceptJudgeVerdict(resp judgeVerdict, covered bool) (bool, string) {
	reason := strings.TrimSpace(resp.Reason)
	if reason == "" {
		reason = "judge rejected candidate"
	}
	if !resp.Pass {
		return false, reason
	}
	if resp.OriginalScore == nil || resp.CandidateScore == nil {
		return false, "judge missing paired scores: " + reason
	}
	orig, cand := *resp.OriginalScore, *resp.CandidateScore
	if !validJudgeScore(orig) || !validJudgeScore(cand) {
		return false, fmt.Sprintf("judge score out of range: original=%.1f candidate=%.1f: %s", orig, cand, reason)
	}
	minDelta := skillJudgeMinScoreDelta
	if covered {
		// Held-out replay is the real behavioral check for covered skills; the
		// judge only needs to confirm direction, not clear a tall bar.
		minDelta = skillCoveredJudgeMinScoreDelta
	}
	if cand-orig < minDelta {
		return false, fmt.Sprintf("candidate score %.1f did not clear %.1f point improvement margin over original score %.1f: %s", cand, minDelta, orig, reason)
	}
	return true, reason
}

func validJudgeScore(score float64) bool {
	return score >= 0 && score <= 100
}

// evolveResp is the evolver model's verdict: skip, or a changed skill body.
type evolveResp struct {
	Skip   bool   `json:"skip"`
	Reason string `json:"reason,omitempty"`
	// ToolGap pairs a skipped evolve with a tool-repair coding candidate when
	// the producer's failure analysis names a tool capability as the root
	// cause (RSI P4, SkillSmith-adapted — propose-only pairing).
	ToolGap *struct {
		Tool        string `json:"tool,omitempty"`
		Description string `json:"description,omitempty"`
		ProposedFix string `json:"proposed_fix,omitempty"`
	} `json:"tool_gap,omitempty"`
	Changes *struct {
		Description            string `json:"description"`
		NewVersion             string `json:"new_version"`
		TargetSignature        string `json:"target_signature,omitempty"`
		EditedSurface          string `json:"edited_surface,omitempty"`
		ExpectedBehaviorChange string `json:"expected_behavior_change,omitempty"`
		RegressionRisk         string `json:"regression_risk,omitempty"`
		Body                   string `json:"body"`
		// ReproductionCase is the producer-authored case demonstrating the fixed
		// defect: original body FAILS it, candidate body PASSES it. Adopted only
		// after the deterministic gate confirms both directions (SEA Alg 8 —
		// reproduction oracle, RSI P1.5).
		ReproductionCase *struct {
			Description         string   `json:"description,omitempty"`
			RequiredSubstrings  []string `json:"required_substrings,omitempty"`
			ForbiddenSubstrings []string `json:"forbidden_substrings,omitempty"`
			RequiredHeadings    []string `json:"required_headings,omitempty"`
		} `json:"reproduction_case,omitempty"`
	} `json:"changes,omitempty"`
}

// teacherRewrite asks the stronger model to produce a better body after the
// lightweight rewrite failed self-test. Reuses the evolve envelope; returns
// the accepted candidate fields (or an empty Body when the teacher declines).
func (e *Evolver) teacherRewrite(ctx context.Context, teacherClient *llm.Client, teacherModel, skillName, originalContent, failedCandidate, rejectReason string, stats *UsageStats) (acceptedSkillCandidate, error) {
	if teacherClient == nil || strings.TrimSpace(teacherModel) == "" {
		return acceptedSkillCandidate{}, fmt.Errorf("teacher rewrite: teacher not configured")
	}
	validationSection := formatValidationCasesForPrompt(e.validationCasesForPrompt(skillName))
	failurePatternSection := formatFailurePatternsForPrompt(stats)
	userPrompt := fmt.Sprintf(`## 현재 SKILL.md
%s

## 직전 개선 시도 (검증 실패)
%s

## 검증 실패 사유
%s

## 사용 통계
- 총 %d회, 성공 %d, 실패 %d (%.0f%%)
- 최근 에러: %s%s%s

위 실패 사유를 해결한 개선된 SKILL.md body 를 생성하세요. 검증 기준(명확성·실재 도구만·구조 유지·범주 수준·실패패턴 해결)을 모두 만족해야 합니다.`,
		originalContent, failedCandidate, rejectReason,
		stats.TotalUses, stats.SuccessCount, stats.FailureCount, stats.SuccessRate*100,
		formatRecentErrors(stats.RecentErrors),
		failurePatternSection,
		validationSection)

	// Load the evolve prompt once: System message AND the pinned producer
	// version for this teacher call, so an escalated commit attributes the body
	// to the teacher's ACTUAL prompt (not the primary's earlier snapshot).
	evolvePrompt := e.metaLoad(generation.MetaEvolveSystemPrompt, generation.DefaultMetaArtifacts()[generation.MetaEvolveSystemPrompt])
	producer := producerSnapshot{model: teacherModel, evolveVersion: generation.ShortContentVersion(evolvePrompt)}

	// Non-streaming — same glm streaming-JSON unreliability as the producer.
	raw, err := teacherClient.Complete(ctx, llm.ChatRequest{
		Model:    teacherModel,
		Messages: []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:   llm.SystemString(evolvePrompt),
		// Same budget as the producer — the teacher rewrites the same shape.
		MaxTokens:      12288,
		Temperature:    evolveTemperature(),
		Thinking:       e.thinkingOff(teacherModel),
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return acceptedSkillCandidate{}, fmt.Errorf("teacher rewrite LLM call: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return acceptedSkillCandidate{}, nil
	}
	// Robust parse: a long skill body sometimes hits the token cap mid-string
	// ("unexpected end of JSON input") or carries unescaped newlines —
	// UnmarshalLLM recovers truncation + escapes control chars. A salvaged-but-
	// broken body still fails the caller's self-test, so recovery is safe.
	resp, err := jsonutil.UnmarshalLLM[teacherRewriteResp](raw)
	if err != nil {
		return acceptedSkillCandidate{}, fmt.Errorf("teacher rewrite: parse: %w", err)
	}
	if resp.Skip || resp.Changes == nil {
		return acceptedSkillCandidate{}, nil
	}
	audit := withHarnessDimensions(HarnessEditAudit{
		TargetSignature:        strings.TrimSpace(resp.Changes.TargetSignature),
		EditedSurface:          strings.TrimSpace(resp.Changes.EditedSurface),
		ExpectedBehaviorChange: strings.TrimSpace(resp.Changes.ExpectedBehaviorChange),
		RegressionRisk:         strings.TrimSpace(resp.Changes.RegressionRisk),
	})
	return acceptedSkillCandidate{
		Body:        stripEchoedFrontmatter(resp.Changes.Body),
		Description: strings.TrimSpace(resp.Changes.Description),
		Audit:       audit,
		producer:    producer,
	}, nil
}

// teacherRewriteResp is the teacher model's rewrite verdict: skip, or a changed
// skill body.
type teacherRewriteResp struct {
	Skip    bool `json:"skip"`
	Changes *struct {
		Description            string `json:"description"`
		TargetSignature        string `json:"target_signature,omitempty"`
		EditedSurface          string `json:"edited_surface,omitempty"`
		ExpectedBehaviorChange string `json:"expected_behavior_change,omitempty"`
		RegressionRisk         string `json:"regression_risk,omitempty"`
		Body                   string `json:"body"`
		// ReproductionCase is the producer-authored case demonstrating the fixed
		// defect: original body FAILS it, candidate body PASSES it. Adopted only
		// after the deterministic gate confirms both directions (SEA Alg 8 —
		// reproduction oracle, RSI P1.5).
		ReproductionCase *struct {
			Description         string   `json:"description,omitempty"`
			RequiredSubstrings  []string `json:"required_substrings,omitempty"`
			ForbiddenSubstrings []string `json:"forbidden_substrings,omitempty"`
			RequiredHeadings    []string `json:"required_headings,omitempty"`
		} `json:"reproduction_case,omitempty"`
	} `json:"changes,omitempty"`
}
