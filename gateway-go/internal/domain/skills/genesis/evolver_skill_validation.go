// evolver_skill_validation.go coordinates candidate validation and escalation.
// Deterministic SKILL.md edit checks live in the guardrails child package.
package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/guardrails"
)

func validateSelfHarnessAudit(audit HarnessEditAudit, stats *UsageStats, reviewFinding string) (bool, string) {
	missing := make([]string, 0, 4)
	if strings.TrimSpace(audit.TargetSignature) == "" {
		missing = append(missing, "target_signature")
	}
	if strings.TrimSpace(audit.EditedSurface) == "" {
		missing = append(missing, "edited_surface")
	}
	if strings.TrimSpace(audit.ExpectedBehaviorChange) == "" {
		missing = append(missing, "expected_behavior_change")
	}
	if strings.TrimSpace(audit.RegressionRisk) == "" {
		missing = append(missing, "regression_risk")
	}
	if len(missing) > 0 {
		return false, "self-harness audit rejected: missing " + strings.Join(missing, ", ")
	}

	// Review findings are already scoped, externalized evidence from the review
	// fork. They may not have a mined terminal=... signature, but they still need
	// a complete audit so the transition remains queryable and rollbackable.
	if strings.TrimSpace(reviewFinding) != "" {
		return true, ""
	}

	patterns := mineSkillFailurePatterns(stats)
	if len(patterns) == 0 {
		return false, "self-harness audit rejected: no failure evidence bundle or review finding supports target_signature"
	}
	target := guardrails.NormalizeSignature(audit.TargetSignature)
	for _, pattern := range patterns {
		if guardrails.SignatureMatches(target, guardrails.NormalizeSignature(pattern.Signature)) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("self-harness audit rejected: target_signature %q does not match supported failure signatures: %s",
		audit.TargetSignature, strings.Join(supportedFailureSignatures(patterns), ", "))
}

func supportedFailureSignatures(patterns []skillFailurePattern) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if signature := strings.TrimSpace(pattern.Signature); signature != "" {
			out = append(out, signature)
		}
	}
	return out
}

func normalizedSelfHarnessSignature(value string) string {
	return guardrails.NormalizeSignature(value)
}

func selfHarnessSignatureMatches(target, supported string) bool {
	return guardrails.SignatureMatches(target, supported)
}

func canonicalSkillSurface(surface string) string {
	return guardrails.CanonicalSkillSurface(surface)
}

type acceptedSkillCandidate struct {
	Body        string
	Description string
	Audit       HarnessEditAudit
}

// selfTestAndMaybeEscalate judges a candidate rewrite. On pass it returns the
// candidate. On fail it escalates to the teacher model (if wired) for one more
// attempt, then re-judges. ok=false means the caller must keep the original
// skill untouched.
func (e *Evolver) selfTestAndMaybeEscalate(ctx context.Context, entry *skills.SkillEntry, originalContent, candidateBody string, stats *UsageStats, audit HarnessEditAudit, reviewFinding string, prov *evolveProvenance) (acceptedSkillCandidate, bool, string) {
	teacherClient, teacherModel := e.teacherModelSnapshot()
	hasTeacher := teacherClient != nil && teacherModel != ""

	// Judge != producer. The candidate came from the lightweight model, so a
	// lightweight judge would be grading its own output — same-family /
	// self-preference bias skews toward accepting it (LLM-judge survey
	// arXiv:2508.02994). pickCandidateJudge routes to the teacher when wired.
	judgeClient, judgeModel := e.pickCandidateJudge()
	// JudgeModel/JudgeArtifactVersion are stamped inside judgeCandidate at each
	// verdict (last wins), so the record reflects the model that judged the
	// COMMITTED body — on escalation that is the primary re-judging the teacher
	// rewrite, not this first judge.
	pass, reason, err := e.validateCandidate(ctx, entry.Skill.Name, judgeClient, judgeModel, originalContent, candidateBody, stats, audit, reviewFinding, prov)
	if err != nil {
		e.logger.Warn("evolver: self-test errored, keeping original",
			"skill", entry.Skill.Name, "error", err)
		return acceptedSkillCandidate{}, false, "judge error"
	}
	if pass {
		return acceptedSkillCandidate{Body: candidateBody, Audit: audit}, true, reason
	}
	e.logger.Info("evolver: self-test rejected lightweight rewrite",
		"skill", entry.Skill.Name, "reason", reason)

	// Teacher-escalation: let the stronger model rewrite once.
	if !hasTeacher {
		return acceptedSkillCandidate{}, false, reason
	}
	teacherCandidate, terr := e.teacherRewrite(ctx, teacherClient, teacherModel, entry.Skill.Name, originalContent, candidateBody, reason, stats)
	if terr != nil || strings.TrimSpace(teacherCandidate.Body) == "" {
		e.logger.Warn("evolver: teacher escalation failed",
			"skill", entry.Skill.Name, "error", terr)
		return acceptedSkillCandidate{}, false, "teacher escalation failed"
	}
	// This rewrite came from the teacher, so judge it with the lightweight model
	// — again keeping judge != producer rather than letting the teacher rubber-
	// stamp its own rewrite. A weaker judge may false-reject a good rewrite, but
	// the loop is fail-closed (keeps the original), so that errs safe.
	primaryClient, primaryModel := e.primaryModel()
	tpass, treason, tjerr := e.validateCandidate(ctx, entry.Skill.Name, primaryClient, primaryModel, originalContent, teacherCandidate.Body, stats, teacherCandidate.Audit, reviewFinding, prov)
	if tjerr != nil || !tpass {
		e.logger.Info("evolver: teacher rewrite still failed self-test",
			"skill", entry.Skill.Name, "reason", treason)
		return acceptedSkillCandidate{}, false, "teacher: " + treason
	}
	e.logger.Info("evolver: teacher escalation succeeded", "skill", entry.Skill.Name)
	// The committed body is the TEACHER's rewrite, not the primary producer's, so
	// correct the provenance producer attribution (the producer snapshot seeded
	// EvolveModel to the primary that made the REJECTED candidate). Pure record
	// side-effect — the accept/reject decision above is unchanged. Without this,
	// teacher-authored evolves are miscredited to the lightweight/coding model.
	if prov != nil {
		prov.EvolveModel = teacherModel
	}
	return teacherCandidate, true, treason
}

func (e *Evolver) validateCandidate(ctx context.Context, skillName string, client *llm.Client, model, originalContent, candidateBody string, stats *UsageStats, audit HarnessEditAudit, reviewFinding string, prov *evolveProvenance) (pass bool, reason string, err error) {
	if ok, reason := e.validateCandidatePreflight(skillName, originalContent, candidateBody, audit, stats, reviewFinding); !ok {
		return false, reason, nil
	}
	return e.judgeCandidate(ctx, skillName, client, model, originalContent, candidateBody, stats, prov)
}

func (e *Evolver) validateCandidatePreflight(skillName, originalContent, candidateBody string, audit HarnessEditAudit, stats *UsageStats, reviewFinding string) (bool, string) {
	// covered means a REAL regression check is active: at least one case the
	// held-out gate can score (hasAssertions). Mere case existence let an
	// assertion-less corpus grant the relaxed caps while the gate failed open.
	covered := hasScorableValidationCase(e.validationCasesForCoverage(skillName))
	if ok, reason := guardrails.ValidateHermesEvolutionGuardrails(originalContent, candidateBody, covered); !ok {
		return false, reason
	}
	if ok, reason := guardrails.ValidateTextualEditBudget(originalContent, candidateBody, covered); !ok {
		return false, reason
	}
	if engine := e.SkillValidationEngine(); engine != nil {
		result, err := engine.ValidateCandidate(skillName, originalContent, candidateBody)
		if err != nil {
			// Fail CLOSED on ANY engine error — do NOT condition on `covered`.
			// `covered` is derived from the SAME validation-store read the engine
			// uses (validationCasesForCoverage → RecentSkillValidationCases →
			// jsonlstore.Load(t.validationPath)), so a store read failure makes
			// covered=false AND errors the engine, and a `if covered` gate would
			// wave the candidate straight through — the exact fail-OPEN M2 meant
			// to close (3rd-review defect). An engine error is specifically a
			// store IO/scan failure: an ABSENT validation file returns (nil,nil)
			// with no error (jsonlstore os.IsNotExist), so a genuinely uncovered
			// skill still proceeds — only an unreadable EXISTING store rejects.
			if e.logger != nil {
				e.logger.Warn("evolver: held-out validation engine error — failing closed",
					"skill", skillName, "error", err)
			}
			return false, "held-out validation engine error (failing closed): " + err.Error()
		} else if result.Evaluated && !result.Pass {
			return false, result.Reason
		}
	}
	if ok, reason := validateSelfHarnessAudit(audit, stats, reviewFinding); !ok {
		return false, reason
	}
	if ok, reason := guardrails.ValidateEditedSurface(audit, originalContent, candidateBody); !ok {
		return false, reason
	}
	return true, ""
}

func (e *Evolver) SkillValidationEngine() *SkillValidationEngine {
	if e == nil || e.tracker == nil {
		return nil
	}
	if e.validationEngine != nil {
		return e.validationEngine
	}
	return NewSkillValidationEngine(e.tracker, e.logger)
}

// heldOutSelectionMargin scores a candidate body's held-out validation
// improvement over the original (candidate score - original score) for the
// K-candidate selector's ranking (#3). It is the deterministic, judge-free
// margin: a candidate that satisfies more held-out forbidden/required assertions
// ranks higher. Returns 0 when there is no engine, no cases, or the gate could
// not evaluate — so uncovered skills tie and fall back to first-committable
// order. Never blocks: an engine error is logged and treated as a 0 margin.
func (e *Evolver) heldOutSelectionMargin(skillName, originalContent, candidateBody string) float64 {
	engine := e.SkillValidationEngine()
	if engine == nil {
		return 0
	}
	result, err := engine.ValidateCandidate(skillName, originalContent, candidateBody)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: held-out selection margin unavailable",
				"skill", skillName, "error", err)
		}
		return 0
	}
	if !result.Evaluated {
		return 0
	}
	return result.CandidateScore - result.OriginalScore
}

// hasScorableValidationCase reports whether any case carries an assertion the
// held-out/behavioral gate can actually score — the honest test for "covered".
func hasScorableValidationCase(cases []SkillValidationCaseRecord) bool {
	for _, tc := range cases {
		if tc.hasAssertions() {
			return true
		}
	}
	return false
}

// validationCasesForPrompt returns recent VISIBLE-pool cases only — what the
// producer/judge/teacher prompts may show as the behavior contract. Blind
// held-out cases never reach an optimization-side prompt, so the gates keep
// scoring assertions the candidate was never shown (gate-echo prevention).
func (e *Evolver) validationCasesForPrompt(skillName string) []SkillValidationCaseRecord {
	if e == nil || e.tracker == nil {
		return nil
	}
	cases, err := e.tracker.recentSkillValidationCasesPool(skillName, skillEvolutionPromptCaseLimit, false)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: validation cases unavailable for prompt",
				"skill", skillName, "error", err)
		}
		return nil
	}
	return cases
}

// validationCasesForCoverage spans BOTH pools: coverage ("does a scorable case
// exist for this skill?") must count blind cases the prompt deliberately hides,
// otherwise a blind-only skill would wrongly take the uncovered relaxed caps.
func (e *Evolver) validationCasesForCoverage(skillName string) []SkillValidationCaseRecord {
	if e == nil || e.tracker == nil {
		return nil
	}
	cases, err := e.tracker.RecentSkillValidationCases(skillName, skillEvolutionPromptCaseLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: validation cases unavailable for coverage",
				"skill", skillName, "error", err)
		}
		return nil
	}
	return cases
}
