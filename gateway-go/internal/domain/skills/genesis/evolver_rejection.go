package genesis

import (
	"fmt"
	"strings"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/guardrails"
)

// Rejection capture split out of evolver.go (pure move, no behavior change):
// lifecycle-log rejected edits, queue self-correction validation drafts, and
// classify rejection reasons (self-harness/replay, patch-first).

func (e *Evolver) recordRejectedSkillEdit(skillName, candidateBody, reason, source string, audit HarnessEditAudit) {
	if e.tracker == nil {
		return
	}
	if err := e.tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName:        skillName,
		Reason:           reason,
		CandidateBody:    candidateBody,
		Source:           source,
		SelfHarnessAudit: audit.Ptr(),
	}); err != nil && e.logger != nil {
		e.logger.Warn("evolver: rejected edit record failed",
			"skill", skillName, "error", err)
	}
	e.queueRejectedEvolveValidationDraft(skillName, reason, source, audit)
	e.queueRepeatedPatchFirstReviewDraft(skillName, reason, source)
	e.queueHeldOutTieCorpusDraft(skillName, reason, source)
}

// rejectedEvolveDraftSource is the shared dedup signature for held-out
// validation drafts: the full Source is this prefix + ":" + the rejection
// source, and the reopen guard matches on the prefix.
const rejectedEvolveDraftSource = "self-harness-rejected-evolve"

func (e *Evolver) queueRejectedEvolveValidationDraft(skillName, reason, source string, audit HarnessEditAudit) {
	reason = strings.TrimSpace(reason)
	skillName = strings.TrimSpace(skillName)
	if reason == "" || skillName == "" || e.tracker == nil {
		return
	}
	if !isSelfHarnessOrReplayRejection(reason) {
		return
	}
	// Dedup: one open held-out draft per skill. Without this every rejected
	// evolve for the same skill minted a fresh near-identical draft (4 duplicates
	// observed in prod), clogging the self-correction queue. The sibling
	// patch-first promoter already guards this — mirror it: a live/operator-ruled
	// twin blocks, and only an APPLIED draft whose signature recurs after the
	// cooldown ("the held-out case never stuck") re-opens.
	if existing, err := e.tracker.RecentSelfCorrectionCandidates(skillName, "", 50); err == nil {
		if selfCorrectionReopenBlocked(existing, rejectedEvolveDraftSource, time.Now().UnixMilli(), time.Now()) {
			return
		}
	}
	target := strings.TrimSpace(audit.TargetSignature)
	if target == "" {
		target = "rejected skill evolution"
	}
	evidence := strings.TrimSpace(reason)
	if auditPtr := audit.Ptr(); auditPtr != nil {
		evidence = strings.TrimSpace(evidence + "\nself_harness_audit=" + selfHarnessAuditSummary(*auditPtr))
	}
	if _, err := e.tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:     "test",
		SkillName: strings.TrimSpace(skillName),
		Title:     "Promote rejected evolve into held-out validation",
		Candidate: "Rejected evolve should become a validation case for " + target,
		Evidence:  evidence,
		Reason:    reason,
		// The edit surface is the validation-case DATA, not the replay engine:
		// acceptance-machinery source is a forbidden self-edit target (surfaces
		// quarantine), and pointing at it got these drafts rejected at record
		// time. The engine file stays referenced in prose only.
		TargetFiles: []string{
			"~/.deneb/data/skill_validation_cases.jsonl",
		},
		ProposedChange: "Review the rejected candidate and add a held-out SkillValidationCaseRecord (validation_replay.go contract) that fails the weak rewrite before any similar evolve is allowed.",
		Risk:           "Do not auto-apply the rejected body; only convert stable observed behavior into a test/replay assertion.",
		Source:         rejectedEvolveDraftSource + ":" + strings.TrimSpace(source),
	}); err != nil && e.logger != nil {
		e.logger.Warn("evolver: rejected evolve validation draft failed",
			"skill", skillName, "error", err)
	}
}

func isSelfHarnessOrReplayRejection(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(reason, "self-harness") ||
		strings.Contains(reason, "held-out") ||
		strings.Contains(reason, "replay") ||
		strings.Contains(reason, "validation")
}

// infrastructureRejectionReasons are the outcomes where NOTHING was judged: the
// judge call itself errored, or the teacher rewrite never produced a body. The
// evolve loop is fail-closed so these correctly keep the original skill — but
// they are OUTAGES, not verdicts on the candidate, and every consumer that
// reads a rejection as "this mutation was bad" is being told something the loop
// never measured.
//
// Live 2026-07-25: 4 of the 14 rejections in the trailing week were literally
// `judge error`. #4200 has since added transient retries, which lowers the rate
// but not the misclassification — the row shape is the bug, not the frequency.
// The sibling treatment already exists one layer over: judgeAccuracyProbeUsable
// drops restart/warmup error storms with the note "not L3 fuel". This is that
// same rule for the evolve ledger.
var infrastructureRejectionReasons = []string{
	"judge error",
	"teacher escalation failed",
}

func isInfrastructureRejection(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	for _, marker := range infrastructureRejectionReasons {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

// Repeated patch-first rejections: the held-out draft above deliberately skips
// Hermes patch-first gate rejections — they carry no replayable behavior
// contract to preserve, only "the rewrite was too broad". But when the SAME
// skill trips that gate repeatedly in a short window, the evolve loop is stuck
// generating oversized rewrites and the funnel should surface one structural
// review. The repeat threshold plus per-skill dedup keep the original
// capture-precision philosophy: a single flaky rewrite never promotes, and a
// signature the operator has already seen never promotes again.
const (
	skillPatchFirstRepeatWindow    = 7 * 24 * time.Hour
	skillPatchFirstRepeatThreshold = 2
	skillPatchFirstRepeatScanLimit = 10
	// skillPatchFirstRepeatSource doubles as the per-skill dedup signature.
	// makeSelfCorrectionID embeds CreatedAt, so identical content still mints
	// a fresh ID on every trigger — the store cannot dedupe re-promotions by
	// ID; the caller must prefix-match this marker across existing candidates.
	skillPatchFirstRepeatSource = "patch-first-repeat-evolve"
)

// isHermesPatchFirstRejection matches both patch-first gate variants (byte cap
// and changed-section cap) and deliberately not the semantic-preservation gate:
// title drift is a different failure class.
func isHermesPatchFirstRejection(reason string) bool {
	return strings.Contains(strings.ToLower(reason), "hermes patch-first gate rejected")
}

// queueRepeatedPatchFirstReviewDraft promotes repeated Hermes patch-first gate
// rejections into one structural review candidate. Intentionally NOT the
// held-out validation template: the rejected bodies prove nothing about desired
// runtime behavior, they prove the evolve path keeps proposing rewrites wider
// than the gate allows — what needs review is the skill's section layout or
// the evolve prompt's section-cap guidance, not a new validation case.
func (e *Evolver) queueRepeatedPatchFirstReviewDraft(skillName, reason, source string) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" || e.tracker == nil || !isHermesPatchFirstRejection(reason) {
		return
	}
	recent, err := e.tracker.RecentRejectedSkillEdits(skillName, skillPatchFirstRepeatScanLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: patch-first repeat scan failed", "skill", skillName, "error", err)
		}
		return
	}
	// The triggering rejection was appended by the caller, so it is part of
	// this scan. Distinct events with byte-identical reasons collapse in the
	// tracker's (skill, reason) dedupe — an undercount, erring on the quiet
	// side of the funnel.
	cutoff := time.Now().Add(-skillPatchFirstRepeatWindow).UnixMilli()
	repeats := make([]RejectedSkillEditRecord, 0, len(recent))
	for _, rec := range recent {
		if rec.CreatedAt >= cutoff && isHermesPatchFirstRejection(rec.Reason) {
			repeats = append(repeats, rec)
		}
	}
	if len(repeats) < skillPatchFirstRepeatThreshold {
		return
	}
	existing, err := e.tracker.RecentSelfCorrectionCandidates(skillName, "", 50)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: patch-first repeat dedup scan failed", "skill", skillName, "error", err)
		}
		return
	}
	// Shared dedup: a live/operator-ruled twin blocks re-promotion; an APPLIED
	// candidate whose repeats keep coming re-opens after the cooldown (the fix
	// did not stick). freshLastAt = newest triggering repeat.
	var freshLastAt int64
	for _, rec := range repeats {
		if rec.CreatedAt > freshLastAt {
			freshLastAt = rec.CreatedAt
		}
	}
	if selfCorrectionReopenBlocked(existing, skillPatchFirstRepeatSource, freshLastAt, time.Now()) {
		return
	}
	evidence := make([]string, 0, len(repeats)+1)
	evidence = append(evidence, fmt.Sprintf("%d patch-first gate rejections within %dd:",
		len(repeats), int(skillPatchFirstRepeatWindow.Hours()/24)))
	for _, rec := range repeats {
		evidence = append(evidence, "- "+genesiscommon.TruncateRunes(rec.Reason, 300))
	}
	targets := []string{"gateway-go/internal/domain/skills/genesis/generation/prompts.go"}
	if e.catalog != nil {
		if entry, ok := e.catalog.Get(skillName); ok && strings.TrimSpace(entry.Skill.FilePath) != "" {
			targets = append([]string{entry.Skill.FilePath}, targets...)
		}
	}
	if _, err := e.tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:     "prompt",
		SkillName: skillName,
		Title:     "Evolve repeatedly rejected by patch-first gate",
		Candidate: fmt.Sprintf("Evolve for %s repeatedly generated rewrites broader than the Hermes patch-first budget. Review splitting the skill body into smaller sections and/or strengthening the evolve prompt's section-cap guidance so candidates stay within %d changed sections.",
			skillName, guardrails.MaxChangedSections),
		Evidence:       strings.Join(evidence, "\n"),
		Reason:         reason,
		TargetFiles:    targets,
		ProposedChange: "Review the skill's section layout (split broad sections so a targeted patch stays under the changed-section cap) or add explicit section-cap guidance to the evolve prompt for this skill. Do not relax the patch-first gate and do not apply any of the rejected bodies.",
		Risk:           "Review-only structural observation; the gate itself is working as designed. Widening skillHermesMaxChangedSections instead of fixing structure would reopen broad-rewrite risk.",
		Source:         skillPatchFirstRepeatSource + ":" + strings.TrimSpace(source),
	}); err != nil && e.logger != nil {
		e.logger.Warn("evolver: patch-first repeat draft failed",
			"skill", skillName, "error", err)
	}
}

func selfHarnessAuditSummary(audit HarnessEditAudit) string {
	audit = withHarnessDimensions(audit)
	parts := []string{
		"target=" + strings.TrimSpace(audit.TargetSignature),
		"surface=" + strings.TrimSpace(audit.EditedSurface),
		"behavior=" + strings.TrimSpace(audit.ExpectedBehaviorChange),
		"risk=" + strings.TrimSpace(audit.RegressionRisk),
	}
	if audit.PrimaryDimension != "" {
		parts = append(parts, "dimension="+audit.PrimaryDimension)
	}
	if len(audit.SecondaryDimensions) > 0 {
		parts = append(parts, "secondary="+strings.Join(audit.SecondaryDimensions, ","))
	}
	return strings.Join(parts, "; ")
}
