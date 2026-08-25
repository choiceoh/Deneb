package genesis

// Skill+tool bundle pairing — P4 of the RSI roadmap (SkillSmith 2606.01314,
// adapted; first slice, propose-only).
//
// When an evolve attempt's failure analysis names a missing/broken TOOL
// capability as the root cause (the skill body cannot fix it), the producer
// skips the rewrite and declares a tool_gap. Deterministic Go then grounds the
// claim — the named tool must actually appear in the skill's recent failure
// traces — and, when grounded, emits a PAIRED self-correction coding candidate
// (the existing propose-only queue with editable-surface enforcement) and
// records the pairing in the lifecycle ledger. Source code stays a forbidden
// self-edit surface: the pairing produces a reviewed coding candidate, never
// an edit.

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// candidateProcedureRef is the composite procedure token for an L4 candidate an
// evolve run produced — the evolve PRODUCER prompt that emitted it, captured at
// the producer LLM call (not re-read from mutable config afterward, per the L1
// capture-at-use rule). The governing set is the evolve prompt alone: the judge
// does not shape a tool-gap declaration. Empty version → empty ref (a
// deterministically-mined or unknown-producer candidate carries no procedure).
func candidateProcedureRef(evolveVersion string) string {
	if strings.TrimSpace(evolveVersion) == "" {
		return ""
	}
	return generation.ProcedureRefFromVersions(map[string]string{
		generation.MetaEvolveSystemPrompt: evolveVersion,
	})
}

// evolveToolGapPairedType is the lifecycle entry type recording the pairing.
const evolveToolGapPairedType = "evolve_tool_gap_paired"

// logEvolveToolGapPaired records that a skipped evolve was paired with a
// tool-repair coding candidate.
func (t *Tracker) logEvolveToolGapPaired(skillName, tool, candidateID string) error {
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:      evolveToolGapPairedType,
		SkillName: skillName,
		Reason:    fmt.Sprintf("tool=%s candidate=%s", tool, candidateID),
		CreatedAt: time.Now().UnixMilli(),
	})
}

// maybePairToolGap validates a producer-declared tool gap and emits the paired
// coding candidate. Best-effort and non-blocking: a malformed or ungrounded
// declaration is dropped with a debug log; the skip result is unaffected.
func (e *Evolver) maybePairToolGap(skillName string, resp evolveResp, stats *UsageStats, evolveVersion string) {
	gap := resp.ToolGap
	if gap == nil || e.tracker == nil {
		return
	}
	tool := strings.TrimSpace(gap.Tool)
	description := strings.TrimSpace(gap.Description)
	if tool == "" || description == "" {
		return
	}

	// Evidence grounding: the named tool must appear in this skill's recent
	// failure traces — a hallucinated pairing must not reach the coding queue.
	evidence := ""
	if stats != nil {
		for _, tr := range stats.RecentFailureTraces {
			if strings.EqualFold(strings.TrimSpace(tr.ToolName), tool) {
				evidence = strings.TrimSpace(tr.Signature)
				if evidence == "" {
					evidence = common.TruncateRunes(tr.ErrorMsg, 200)
				}
				break
			}
		}
	}
	if evidence == "" {
		e.logger.Debug("evolver: tool gap not grounded in failure traces, dropped",
			"skill", skillName, "tool", tool)
		return
	}

	// One open pairing per (skill, tool): a prior proposed/accepted candidate
	// for the same tool means the queue already knows.
	title := fmt.Sprintf("도구 공백: %s — %s", tool, skillName)
	if existing, err := e.tracker.RecentSelfCorrectionCandidates(skillName, "", 20); err == nil {
		for _, c := range existing {
			if c.Title == title && (c.Status == SelfCorrectionStatusProposed || c.Status == SelfCorrectionStatusAccepted) {
				e.logger.Debug("evolver: tool gap already queued", "skill", skillName, "tool", tool, "id", c.ID)
				return
			}
		}
	}

	record, err := e.tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:          "code",
		SkillName:      skillName,
		Title:          title,
		Candidate:      description,
		ProposedChange: strings.TrimSpace(gap.ProposedFix),
		Evidence:       fmt.Sprintf("evolve skip paired: failure signature %q names tool %s", evidence, tool),
		Reason:         strings.TrimSpace(resp.Reason),
		Source:         "evolve-tool-gap",
		ProcedureRef:   candidateProcedureRef(evolveVersion),
	})
	if err != nil {
		e.logger.Warn("evolver: tool gap candidate record failed",
			"skill", skillName, "tool", tool, "error", err)
		return
	}
	if err := e.tracker.logEvolveToolGapPaired(skillName, tool, record.ID); err != nil {
		e.logger.Warn("evolver: tool gap lifecycle write failed",
			"skill", skillName, "error", err)
	}
	e.logger.Info("evolver: tool gap paired with coding candidate (propose-only)",
		"skill", skillName, "tool", tool, "candidate", record.ID)
}
