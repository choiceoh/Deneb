package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Candidate evaluation and commit split out of evolver.go (pure move, no
// behavior change): run one candidate body through the gates, commit the
// winner, plus model snapshots, version bump, and the evolution-evidence gate.

// evaluatedCandidate is the outcome of running a single candidate body through
// the behavioral + selection + self-test gates WITHOUT committing it. Exactly
// one of result / committable carries the answer: result is set for a skip or a
// gate rejection (already lifecycle-logged), otherwise the candidate is
// committable and the body/version/metadata fields are populated, with margin
// scoring the candidate for the K-candidate selector (#3).
type evaluatedCandidate struct {
	result *EvolveResult // non-nil → skip or rejection, do not commit

	body        string
	newVersion  string
	description string
	audit       HarnessEditAudit
	margin      float64 // held-out score margin (candidate - original); selection rank
	// prov is the evaluator-attribution certificate accumulated while the
	// candidate ran the gates (RSI P1.5) — recorded with the lifecycle entry.
	prov evolveProvenance
	// reproduction is the producer-authored defect-reproduction case, adopted
	// at commit only after the deterministic oracle confirms it (fails on the
	// original body, passes on the committed body).
	reproduction *SkillValidationCaseRecord
}

// evaluateCandidateText parses one producer/teacher response and runs the full
// pre-commit gate stack (behavioral replay → deterministic selection → self-test
// + teacher escalation), returning an evaluatedCandidate. It is the gate half of
// the old parseAndApply, split out so the K-candidate selector (#3) can score
// several candidates before any single one is committed. Behavior for one
// candidate is identical to the previous inline path.
func (e *Evolver) evaluateCandidateText(ctx context.Context, text string, snap producerSnapshot, entry *skills.SkillEntry, originalContent string, stats *UsageStats, reviewFinding string) (evaluatedCandidate, error) {
	resp, err := jsonutil.UnmarshalLLM[evolveResp](text)
	if err != nil {
		return evaluatedCandidate{}, fmt.Errorf("evolver: parse response (len=%d, tail=%q): %w",
			len(text), tailRunes(text, 160), err)
	}

	if resp.Skip || resp.Changes == nil {
		// P4 pairing: a skip whose root cause is a named tool defect emits a
		// paired coding candidate (grounded + deduped inside).
		e.maybePairToolGap(entry.Skill.Name, resp, stats, snap.evolveVersion)
		return evaluatedCandidate{result: &EvolveResult{
			SkillName: entry.Skill.Name,
			Evolved:   false,
			Reason:    resp.Reason,
		}}, nil
	}

	// Reconstruct SKILL.md with updated body.
	header, bodyOffset := skills.ExtractFrontmatterBlock(originalContent)
	if bodyOffset == 0 || header == "" {
		return evaluatedCandidate{}, fmt.Errorf("evolver: skill %q has no valid frontmatter", entry.Skill.Name)
	}

	// Update version in frontmatter. An empty or unchanged version from the
	// LLM still gets a forced patch bump so every committed evolve is
	// distinguishable (production evolves were landing as the same version).
	newVersion := strings.TrimSpace(resp.Changes.NewVersion)
	if newVersion == "" || newVersion == entry.Skill.Version {
		newVersion = bumpPatchVersion(entry.Skill.Version)
	}

	candidateBody := stripEchoedFrontmatter(resp.Changes.Body)
	audit := HarnessEditAudit{
		TargetSignature:        strings.TrimSpace(resp.Changes.TargetSignature),
		EditedSurface:          strings.TrimSpace(resp.Changes.EditedSurface),
		ExpectedBehaviorChange: strings.TrimSpace(resp.Changes.ExpectedBehaviorChange),
		RegressionRisk:         strings.TrimSpace(resp.Changes.RegressionRisk),
	}
	audit = withHarnessDimensions(audit)
	committedDescription := strings.TrimSpace(resp.Changes.Description)
	committedAudit := audit
	prov := provenanceFromProducer(snap)

	// Deterministic repeat refusal BEFORE the gate pipeline: a candidate
	// byte-identical to a recently rejected one re-buys nothing but replay,
	// judge, and held-out cost (measured: the same body ran the full pipeline
	// three times in 08). The rejected-edit buffer's prompt steering is soft;
	// this is the hard stop when the model ignores it.
	if reason, repeat := e.refuseRepeatedRejectedCandidate(entry.Skill.Name, candidateBody); repeat {
		return e.rejectCandidateEdit(entry.Skill.Name, candidateBody, reason, "repeat-refusal", "", audit, &prov), nil
	}

	// Gate order is inviolable: behavioral replay → deterministic selection
	// preflight (self-test-off mode) → LLM self-test + teacher escalation.
	if rejected := e.preSelfTestGates(ctx, entry, originalContent, candidateBody, audit, stats, reviewFinding, &prov); rejected != nil {
		return *rejected, nil
	}

	// Self-test the rewrite before committing it. A failed or uncertain judge
	// keeps the original — a bad "improvement" is worse than no change. When a
	// teacher (main) model is wired, it gets one escalated attempt first (#4).
	if e.selfTest {
		accepted, ok, reason := e.selfTestAndMaybeEscalate(ctx, entry, originalContent, candidateBody, stats, audit, reviewFinding, &prov)
		if !ok {
			return e.rejectCandidateEdit(entry.Skill.Name, candidateBody, reason, "self-test", "self-test rejected: ", audit, &prov), nil
		}
		candidateBody = accepted.Body
		if strings.TrimSpace(accepted.Description) != "" {
			committedDescription = strings.TrimSpace(accepted.Description)
		}
		if !accepted.Audit.Empty() {
			committedAudit = accepted.Audit
		}
	}

	// margin ranks committable candidates in the K-selector (#3). The held-out
	// validation score delta is the deterministic, judge-independent signal; it is
	// computed on the final body (which may be a teacher rewrite) so the rank
	// matches what would actually be committed. It stays 0 when a skill has no
	// held-out cases — all such candidates then tie and fall back to
	// first-committable order, preserving single-candidate behavior.
	margin := e.heldOutSelectionMargin(entry.Skill.Name, originalContent, candidateBody)
	prov.HeldOutMargin = &margin
	reproduction := e.reproductionCaseFromResp(entry.Skill.Name, newVersion, resp, stats)
	return evaluatedCandidate{
		body:         candidateBody,
		newVersion:   newVersion,
		description:  committedDescription,
		audit:        committedAudit,
		margin:       margin,
		prov:         prov,
		reproduction: reproduction,
	}, nil
}

// preSelfTestGates runs the pre-commit gates that come BEFORE the LLM
// self-test, in their inviolable order, returning a non-nil rejection when a
// gate fails (nil → the candidate may proceed to self-test):
//
//  1. Execution-grounded behavioral gate (do-no-harm safety net). Replays the
//     candidate vs the original through the executor model on stored replay
//     cases and rejects a candidate that regresses the proven tool-call
//     behavior. Orthogonal to the self-test/judge, so it runs in both modes.
//     Fail-open: disabled, no cases, or executor error never blocks.
//  2. Deterministic selector gates (self-test-off mode only) are not optional:
//     even when LLM self-testing is disabled for cost/latency, candidates must
//     still obey bounded edit and held-out validation constraints. (With
//     self-test on, validateCandidatePreflight runs inside
//     selfTestAndMaybeEscalate — unchanged.)
func (e *Evolver) preSelfTestGates(ctx context.Context, entry *skills.SkillEntry, originalContent, candidateBody string, audit HarnessEditAudit, stats *UsageStats, reviewFinding string, prov *evolveProvenance) *evaluatedCandidate {
	if behavior, berr := e.validationEngine.EvaluateBehavior(ctx, entry.Skill.Name, skillBodyOnly(originalContent), candidateBody); berr != nil {
		e.logger.Warn("evolver: behavioral replay unavailable, skipping gate",
			"skill", entry.Skill.Name, "error", berr)
	} else if behavior.Evaluated && !behavior.Pass {
		rejected := e.rejectCandidateEdit(entry.Skill.Name, candidateBody, behavior.Reason, "behavioral-replay", "behavioral replay rejected: ", audit, prov)
		return &rejected
	}

	if !e.selfTest {
		if ok, reason := e.validateCandidatePreflight(entry.Skill.Name, originalContent, candidateBody, audit, stats, reviewFinding); !ok {
			rejected := e.rejectCandidateEdit(entry.Skill.Name, candidateBody, reason, "preflight", "selection rejected: ", audit, prov)
			return &rejected
		}
	}
	return nil
}

// rejectCandidateEdit records a gate rejection — best-effort lifecycle record
// (so rejected attempts are visible in the native observability feed, not just
// operator logs) plus the rejected-edit buffer — and returns the
// non-committable evaluatedCandidate. Pure extraction of the three identical
// rejection tails (behavioral replay / selection preflight / self-test);
// source labels the buffer entry and reasonPrefix the caller-visible Reason,
// both exactly as before.
func (e *Evolver) rejectCandidateEdit(skillName, candidateBody, reason, source, reasonPrefix string, audit HarnessEditAudit, prov *evolveProvenance) evaluatedCandidate {
	if e.tracker != nil {
		if logErr := e.tracker.logEvolveRejectedWithProvenance(skillName, reason, audit, prov); logErr != nil {
			e.logger.Warn("evolver: lifecycle log write failed",
				"skill", skillName, "error", logErr)
		}
	}
	e.recordRejectedSkillEdit(skillName, candidateBody, reason, source, audit)
	return evaluatedCandidate{result: &EvolveResult{
		SkillName: skillName,
		Evolved:   false,
		Reason:    reasonPrefix + reason,
	}}
}

// reproductionCaseFromResp builds the producer-authored defect-reproduction
// case from the parsed evolve response (resp.Changes must be non-nil — callers
// sit past the skip gate), or nil when the producer supplied none. Behavioral
// enrichment (①): string assertions only test "does the body SAY X". When the
// targeted failure named a concrete tool call, attach a behavioral replay
// assertion so the strongest gate (executor-based EvaluateBehavior) also tests
// "does the skill make the agent DO X". Free (tool data already lives in the
// failure trace); the commit-time oracle scores strings only, so this never
// corrupts the fails-on-original test.
func (e *Evolver) reproductionCaseFromResp(skillName, newVersion string, resp evolveResp, stats *UsageStats) *SkillValidationCaseRecord {
	rc := resp.Changes.ReproductionCase
	if rc == nil {
		return nil
	}
	reproduction := &SkillValidationCaseRecord{
		SkillName:           skillName,
		ID:                  fmt.Sprintf("repro-%s-%s", skillName, newVersion),
		Description:         strings.TrimSpace(rc.Description),
		RequiredSubstrings:  rc.RequiredSubstrings,
		ForbiddenSubstrings: rc.ForbiddenSubstrings,
		RequiredHeadings:    rc.RequiredHeadings,
		Source:              "reproduction-oracle",
		FrontierTier:        "hard",
	}
	enrichReproductionWithBehavior(reproduction, stats)
	return reproduction
}

// commitEvaluatedCandidate writes an already-gated candidate to disk and records
// the lifecycle/observability tail. It is the commit half of the old
// parseAndApply, unchanged in behavior.
func (e *Evolver) commitEvaluatedCandidate(entry *skills.SkillEntry, originalContent string, eval evaluatedCandidate) (*EvolveResult, error) {
	header, _ := skills.ExtractFrontmatterBlock(originalContent)
	newVersion := eval.newVersion
	candidateBody := eval.body
	committedDescription := eval.description
	committedAudit := eval.audit

	// Guard the empty-version case: strings.Replace with an empty "old"
	// would prepend newVersion to the header instead of replacing anything.
	newHeader := header
	if entry.Skill.Version != "" {
		newHeader = strings.Replace(header, entry.Skill.Version, newVersion, 1)
	}
	newContent := newHeader + "\n" + candidateBody + "\n"

	// Back up the pre-evolve content before overwriting so a regressing evolve
	// can be rolled back (post-evolve watch in the tracker). Best-effort: a
	// backup failure must not block the evolve, but it disables rollback for
	// this version until the next evolve refreshes the backup.
	if err := backupSkillVersion(entry.Skill.FilePath, originalContent); err != nil {
		e.logger.Warn("evolver: skill backup failed, rollback disabled for this evolve",
			"skill", entry.Skill.Name, "error", err)
	}

	// Write back atomically so a crash mid-write can't corrupt the skill.
	if err := atomicfile.WriteFile(entry.Skill.FilePath, []byte(newContent), &atomicfile.Options{Perm: 0o644}); err != nil {
		return nil, fmt.Errorf("evolver: write file: %w", err)
	}
	if e.catalog != nil {
		updated := *entry
		updated.Skill.Version = newVersion
		e.catalog.Register(updated)
	}

	e.logger.Info(
		"evolver: skill evolved",
		"skill", entry.Skill.Name,
		"version", newVersion,
		"description", committedDescription,
	)

	// Durable lifecycle record for the evolution timeline. The curator's
	// MarkSkillPatched only tracks agent-created skills, so without this a
	// committed evolve of a user-authored skill leaves no queryable trace.
	if e.tracker != nil {
		if logErr := e.tracker.logEvolveWithProvenance(entry.Skill.Name, newVersion, committedDescription, committedAudit, &eval.prov); logErr != nil {
			e.logger.Warn("evolver: lifecycle log write failed",
				"skill", entry.Skill.Name, "error", logErr)
		}
	}

	// Reproduction oracle (SEA Alg 8, RSI P1.5): adopt the producer-authored
	// defect-reproduction case only after deterministically confirming it fails
	// on the original body and passes on the committed body — solving the
	// zero-validation-case cold start exactly where evolves happen. Best-effort
	// and non-blocking; the weak-case filter still applies on record.
	e.adoptReproductionCase(entry.Skill.Name, originalContent, candidateBody, eval.reproduction)

	// Cross-skill regression sweep (#4). Replays the just-evolved skill's held-out
	// assertions against its most similar neighbors and surfaces any neighbor that
	// now violates the new forbidden/required contract. Best-effort and
	// non-blocking: it never rolls back or fails the evolve, only observes.
	e.detectCrossSkillRegression(entry.Skill.Name)

	result := EvolveResult{
		SkillName:    entry.Skill.Name,
		Evolved:      true,
		NewVersion:   newVersion,
		Description:  committedDescription,
		Audit:        committedAudit.Ptr(),
		JudgeVersion: eval.prov.JudgeArtifactVersion,
	}
	if eval.prov.JudgeScoreOriginal != nil && eval.prov.JudgeScoreCandidate != nil {
		margin := *eval.prov.JudgeScoreCandidate - *eval.prov.JudgeScoreOriginal
		result.JudgeMargin = &margin
		result.NeedsOperatorVerdict = margin <= skillLowConfidenceJudgeMaxDelta
	}
	if result.NeedsOperatorVerdict {
		e.notifyLowConfidence(result)
	}
	return &result, nil
}

// adoptReproductionCase runs the deterministic reproduction oracle: the case is
// recorded as a validation case only when the original body FAILS it and the
// committed candidate body PASSES it. Anything else — vacuous case, passes on
// the original (non-discriminative), fails on the candidate (mis-authored) — is
// dropped with a debug log. LLM produces, Go verifies.
func (e *Evolver) adoptReproductionCase(skillName, originalContent, candidateBody string, rc *SkillValidationCaseRecord) {
	if rc == nil || e.tracker == nil {
		return
	}
	// The fails-on-original / passes-on-candidate determination scores STRING
	// assertions only. Behavioral replay assertions (attached by
	// enrichReproductionWithBehavior) are for the executor gate — scoring them
	// against the static body trace here would wrongly drop a good case when the
	// candidate legitimately still names the failing tool (the fix is usually a
	// corrected call, not tool removal). They ride onto the recorded case below.
	stringOnly := *rc
	stringOnly.Replay = SkillReplayCaseRecord{}
	cases := []SkillValidationCaseRecord{stringOnly}
	origScore := scoreSkillValidationCases(skillBodyOnly(originalContent), cases)
	candScore := scoreSkillValidationCases(candidateBody, cases)
	if origScore.Total == 0 {
		e.logger.Debug("evolver: reproduction case vacuous, dropped", "skill", skillName)
		return
	}
	if origScore.Passed == origScore.Total {
		e.logger.Debug("evolver: reproduction case passes on original (non-discriminative), dropped",
			"skill", skillName)
		return
	}
	if candScore.Passed != candScore.Total {
		e.logger.Debug("evolver: reproduction case fails on candidate (mis-authored), dropped",
			"skill", skillName)
		return
	}
	if err := e.tracker.RecordSkillValidationCase(*rc); err != nil {
		e.logger.Debug("evolver: reproduction case rejected by tracker filter",
			"skill", skillName, "error", err)
		return
	}
	e.logger.Info("evolver: reproduction case adopted (fails-on-original, passes-on-candidate confirmed)",
		"skill", skillName, "case", rc.ID)
}

func (e *Evolver) primaryModel() (*llm.Client, string) {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	model := e.model
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return e.llmClient, model
}

func (e *Evolver) teacherModelSnapshot() (*llm.Client, string) {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.teacherClient, e.teacherModel
}

func (e *Evolver) judgeModelSnapshot() (*llm.Client, string) {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.judgeClient, e.judgeModel
}

// stripEchoedFrontmatter removes any leading YAML frontmatter block(s) an LLM
// echoed into a rewritten skill body. The evolve prompt asks for the body
// only, but lightweight models often return the whole SKILL.md; blindly
// prepending the canonical header then stacks one duplicate frontmatter per
// evolve cycle (production email-analysis reached a triple frontmatter this
// way). Only blocks that parse as skill frontmatter (a name: key) are
// stripped, so a body that legitimately opens with a "---" rule is kept. The
// bodyOffset >= len guard also keeps the no-closing-delimiter case (where the
// whole text is treated as frontmatter) from stripping the body to nothing.
func stripEchoedFrontmatter(body string) string {
	for {
		header, bodyOffset := skills.ExtractFrontmatterBlock(body)
		if header == "" || bodyOffset <= 0 || bodyOffset >= len(body) {
			return body
		}
		if skills.ParseFrontmatter(body)["name"] == "" {
			return body
		}
		body = strings.TrimLeft(body[bodyOffset:], "\n")
	}
}

// bumpPatchVersion increments the patch segment of a semver string. It preserves
// the major.minor lineage even for a loosely-semver version (a 2-part "1.0" or a
// non-numeric patch): a missing/unparseable patch is treated as 0 and bumped to
// 1. Only a version with no usable major.minor falls back to the genesis seed
// "0.1.1" — the old code reset every non-3-part version to "0.1.1", silently
// dropping a skill's version lineage on its first evolve (#12).
func bumpPatchVersion(version string) string {
	version = strings.TrimSpace(version)
	parts := strings.Split(version, ".")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "0.1.1"
	}
	patch := 0
	if len(parts) >= 3 {
		// A non-numeric patch (e.g. a "-rc1" suffix) leaves patch at 0 → bumps to
		// 1, keeping major.minor rather than discarding the whole version.
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &patch); err != nil {
			patch = 0
		}
	}
	return fmt.Sprintf("%s.%s.%d", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), patch+1)
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

const (
	skillJudgeMinScoreDelta = 2.0
	// Borderline accepted candidates ship behind the existing rollback watch,
	// but request an operator label for P3 verifier co-evolution.
	skillLowConfidenceJudgeMaxDelta   = 3.0
	skillEvolutionMinEvidenceUses     = 2
	skillEvolutionMinEvidenceFailures = 2
	skillEvolutionPromptCaseLimit     = 5
	skillFailurePatternLimit          = 4
)

// Coverage-conditional gate relaxation (2026-07-09, operator-approved measured
// bets): a skill WITH held-out validation cases has a real behavioral
// regression check, so the size/judge proxies that stood in for measurement can
// loosen — bigger rewrites become measured bets instead of blind ones. Skills
// with no coverage keep the conservative caps above. The backfill lane
// (validation_backfill_task.go) grows coverage, so skills graduate to the
// relaxed tier as their corpus fills.
const (
	skillCoveredJudgeMinScoreDelta = 1.0
)

// skillEvolveBurstMaxRounds bounds loop-until-dry evolution per skill per
// cycle (first round + up to two burst rounds). Each accepted round already
// paid the full gate stack, so the cap is a cost bound, not a safety one —
// safety stays with the gates and the thrash breaker.
const skillEvolveBurstMaxRounds = 3

// skillEvolveCandidateCount is K for the multi-candidate generate-and-select
// path (#3): the evolver streams this many candidate bodies from the producer
// (each after the first nudged by a small variation note so they differ), runs
// the full per-candidate gate stack on each, and commits the best non-regressive
// one. K=1 preserves the original single-candidate behavior exactly. Kept small
// so a background evolve cycle's LLM-call budget grows only linearly.
const skillEvolveCandidateCount = 5

// skillUncoveredJudgeMinScoreDelta is the judge score margin required to accept
// an evolve of a skill that has NO held-out validation cases (#5). It stays
// larger than the covered margin (skillJudgeMinScoreDelta) because the held-out
// gate fails open with zero cases, leaving the judge verdict as the only
// behavioral check — an uncovered skill is still harder to rewrite. It was
// lowered from 6.0 to 3.0 (aggressive proactive-refinement mode): in production
// NO skill has validation cases, so 6.0 froze every foundational skill at its
// v1. The other nets (backup, auto-rollback after 3 consecutive failures, thrash
// cooldown, and the qualitative judge rules that catch invented tools / safety
// regressions / non-patch rewrites) bound the risk of the lower margin.
const skillUncoveredJudgeMinScoreDelta = 3.0

func hasSufficientEvolutionEvidence(stats *UsageStats, reviewFinding string) bool {
	if strings.TrimSpace(reviewFinding) != "" {
		return true
	}
	return stats != nil &&
		stats.TotalUses >= skillEvolutionMinEvidenceUses &&
		stats.FailureCount >= skillEvolutionMinEvidenceFailures &&
		(len(stats.RecentErrors) > 0 || len(stats.RecentFailureTraces) > 0)
}

// enrichReproductionWithBehavior attaches a behavioral replay assertion to a
// producer-authored reproduction case when the skill's targeted failure trace
// names a concrete failing tool call. The failed invocation becomes a
// ForbiddenToolCall (the corrected skill should no longer drive the agent into
// that failing call) plus a forbidden-action signature, and the trace supplies
// the task input to simulate — so the case is behaviorally evaluable by the
// executor gate, not just prose-matched. Best-effort and additive: a trace
// without a tool defect leaves the case string-only.
func enrichReproductionWithBehavior(rc *SkillValidationCaseRecord, stats *UsageStats) {
	if rc == nil || stats == nil {
		return
	}
	for _, tr := range stats.RecentFailureTraces {
		tool := strings.TrimSpace(tr.ToolName)
		if tool == "" || !tr.ToolError {
			continue
		}
		call := SkillReplayToolCallRecord{Name: tool, FixtureError: true}
		if frag := strings.TrimSpace(tr.ToolInput); frag != "" {
			call.InputIncludes = []string{genesiscommon.TruncateRunes(frag, 80)}
		}
		rc.Replay.ForbiddenToolCalls = append(rc.Replay.ForbiddenToolCalls, call)
		if sig := strings.TrimSpace(tr.Signature); sig != "" {
			rc.Replay.ForbiddenActions = append(rc.Replay.ForbiddenActions, genesiscommon.TruncateRunes(sig, 80))
		}
		if rc.Replay.Input == "" {
			rc.Replay.Input = "reproduce the failure that named tool " + tool
		}
		return // one behavioral assertion per case is enough signal
	}
}
