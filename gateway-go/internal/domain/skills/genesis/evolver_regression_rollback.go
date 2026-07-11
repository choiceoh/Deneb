package genesis

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Cross-skill regression detection and version backup/rollback split out of
// evolver.go (pure move, no behavior change).

const (
	// skillCrossRegressionMaxNeighbors caps how many similar neighbor skills the
	// post-commit cross-skill regression sweep (#4) scores, so a large catalog
	// never turns one evolve into an unbounded file-read fan-out.
	skillCrossRegressionMaxNeighbors = 3
	// skillCrossRegressionMinSimilarity is the name+description Jaccard floor for
	// treating a skill as a neighbor. Deliberately well below the near-duplicate
	// skillDedupThreshold (0.82): the sweep wants related-but-distinct skills that
	// could share a hazard, not just clones. A shared tag also qualifies.
	skillCrossRegressionMinSimilarity = 0.18
	skillCrossRegressionCaseLimit     = 20
)

// detectCrossSkillRegression runs the just-evolved skill's held-out validation
// cases against its most similar neighbor skills and emits a
// cross_skill_regression observation for any neighbor that now violates the
// evolved skill's forbidden/required assertions (#4). It is best-effort and
// NON-BLOCKING: the evolve is already committed, this never rolls back, and any
// missing piece (no tracker, no catalog, no cases, no neighbors) is a silent
// no-op. The neighbor was never under the evolved skill's contract, so a hit is
// a coupling signal to surface — not proof the edit was wrong.
func (e *Evolver) detectCrossSkillRegression(skillName string) {
	if e == nil || e.tracker == nil || e.catalog == nil {
		return
	}
	cases, err := e.tracker.RecentSkillValidationCases(skillName, skillCrossRegressionCaseLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: cross-skill regression skipped, validation cases unavailable",
				"skill", skillName, "error", err)
		}
		return
	}
	if len(cases) == 0 {
		return
	}
	neighbors := e.crossSkillNeighbors(skillName)
	if len(neighbors) == 0 {
		return
	}
	for _, neighbor := range neighbors {
		body, rerr := os.ReadFile(neighbor.Skill.FilePath)
		if rerr != nil {
			if e.logger != nil {
				e.logger.Warn("evolver: cross-skill regression skipped neighbor, read failed",
					"skill", skillName, "neighbor", neighbor.Skill.Name, "error", rerr)
			}
			continue
		}
		result := CrossSkillRegression(neighbor.Skill.Name, string(body), cases)
		if !result.Failed {
			continue
		}
		reason := fmt.Sprintf("neighbor %q regressed %d/%d of evolved skill %q's held-out assertions: %s",
			neighbor.Skill.Name, result.Total-result.Passed, result.Total, skillName,
			formatValidationFailures(result.Failures))
		if e.logger != nil {
			e.logger.Warn("evolver: cross-skill regression detected",
				"skill", skillName, "neighbor", neighbor.Skill.Name,
				"failedAssertions", result.Total-result.Passed, "totalAssertions", result.Total)
		}
		if logErr := e.tracker.LogCrossSkillRegression(skillName, neighbor.Skill.Name, reason); logErr != nil && e.logger != nil {
			e.logger.Warn("evolver: cross-skill regression lifecycle log write failed",
				"skill", skillName, "neighbor", neighbor.Skill.Name, "error", logErr)
		}
	}
}

// crossSkillNeighbors returns up to skillCrossRegressionMaxNeighbors catalog
// skills most similar to skillName, ranked by name+description token Jaccard with
// a shared-tag boost. The evolved skill itself and skills with no resolvable file
// are excluded. Neighbors below skillCrossRegressionMinSimilarity that also share
// no tag are dropped, so an unrelated catalog never produces spurious neighbors.
func (e *Evolver) crossSkillNeighbors(skillName string) []skills.SkillEntry {
	if e == nil || e.catalog == nil {
		return nil
	}
	self, ok := e.catalog.Get(skillName)
	if !ok {
		return nil
	}
	selfTokens := genesiscommon.SkillDedupTokens(self.Skill.Name, self.Skill.Description)
	selfTags := skillTagSet(*self)
	if len(selfTokens) == 0 && len(selfTags) == 0 {
		return nil
	}

	type scoredNeighbor struct {
		entry skills.SkillEntry
		score float64
	}
	var scored []scoredNeighbor
	for _, candidate := range e.catalog.List() {
		if candidate.Skill.Name == skillName || strings.TrimSpace(candidate.Skill.FilePath) == "" {
			continue
		}
		similarity := genesiscommon.JaccardSimilarity(selfTokens, genesiscommon.SkillDedupTokens(candidate.Skill.Name, candidate.Skill.Description))
		sharesTag := tagSetsOverlap(selfTags, skillTagSet(candidate))
		if similarity < skillCrossRegressionMinSimilarity && !sharesTag {
			continue
		}
		// A shared tag is a strong coupling hint, so it floors the rank above any
		// purely token-similar neighbor while still letting similarity break ties.
		score := similarity
		if sharesTag {
			score++
		}
		scored = append(scored, scoredNeighbor{entry: candidate, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].entry.Skill.Name < scored[j].entry.Skill.Name
	})
	if len(scored) > skillCrossRegressionMaxNeighbors {
		scored = scored[:skillCrossRegressionMaxNeighbors]
	}
	out := make([]skills.SkillEntry, 0, len(scored))
	for _, n := range scored {
		out = append(out, n.entry)
	}
	return out
}

// skillTagSet returns the lowercased frontmatter tag set for an entry, or nil
// when the skill carries no metadata tags.
func skillTagSet(entry skills.SkillEntry) map[string]struct{} {
	if entry.Metadata == nil || len(entry.Metadata.Tags) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(entry.Metadata.Tags))
	for _, tag := range entry.Metadata.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			set[tag] = struct{}{}
		}
	}
	return set
}

func tagSetsOverlap(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// Iterate the smaller set so the lookup cost is bounded by min(|a|,|b|).
	if len(b) < len(a) {
		a, b = b, a
	}
	for tag := range a {
		if _, ok := b[tag]; ok {
			return true
		}
	}
	return false
}

// skillBackupPath returns the rollback backup path for a skill file. The
// .backups subdir and .prev suffix keep it out of SKILL.md discovery.
func skillBackupPath(skillFile string) string {
	return filepath.Join(filepath.Dir(skillFile), ".backups", filepath.Base(skillFile)+".prev")
}

// backupSkillVersion saves the pre-evolve content next to the skill. One level
// of undo is enough: each evolve overwrites the backup with the then-current
// content, so it always holds the version immediately before the latest evolve.
func backupSkillVersion(skillFile, content string) error {
	backup := skillBackupPath(skillFile)
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(backup, []byte(content), &atomicfile.Options{Perm: 0o644})
}

// RollbackSkill restores the pre-evolve version of a skill from its backup. The
// tracker's post-evolve watch calls this when an evolved skill fails its next
// few uses in a row. It mirrors parseAndApply's write behavior (atomic file
// write + lifecycle log), so the reverted skill propagates the same way an
// evolve does. Best-effort: a missing backup or absent catalog entry is a
// no-op (logged), never a crash.
func (e *Evolver) RollbackSkill(skillName string) {
	if e.catalog == nil {
		return
	}
	entry, ok := e.catalog.Get(skillName)
	if !ok {
		e.logger.Warn("evolver: rollback skipped, skill not in catalog", "skill", skillName)
		return
	}
	prev, err := os.ReadFile(skillBackupPath(entry.Skill.FilePath))
	if err != nil {
		e.logger.Warn("evolver: rollback skipped, no backup available", "skill", skillName, "error", err)
		return
	}
	// Capture the regressing body BEFORE restoring the backup: recording it as
	// a rejected edit (RSI P1.5 ③, CPE 2605.09315) feeds the rejected-edit
	// buffer and recurrence machinery so the exact same bad rewrite cannot be
	// silently re-proposed on the next evolve cycle — previously a rollback
	// left only a lifecycle line and no re-proposal defense. Best-effort.
	rolledBackBody := ""
	if cur, rerr := os.ReadFile(entry.Skill.FilePath); rerr == nil {
		rolledBackBody = skillBodyOnly(string(cur))
	}
	if err := atomicfile.WriteFile(entry.Skill.FilePath, prev, &atomicfile.Options{Perm: 0o644}); err != nil {
		e.logger.Error("evolver: rollback write failed", "skill", skillName, "error", err)
		return
	}
	e.logger.Info("evolver: skill rolled back after consecutive post-evolve failures", "skill", skillName)
	if e.tracker != nil {
		if err := e.tracker.LogEvolveRolledBack(skillName); err != nil {
			e.logger.Warn("evolver: rollback lifecycle log failed", "skill", skillName, "error", err)
		}
		if rolledBackBody != "" {
			e.recordRejectedSkillEdit(skillName, rolledBackBody, "post-evolve rollback: regressed in real use", "rollback", HarnessEditAudit{})
		}
		e.distillRollbackValidationCase(skillName)
	}
}

// distillRollbackValidationCase turns the failure evidence that tripped a
// rollback into a hard-frontier held-out case (RSI P1.5 ③): the next evolve
// of this skill must clear the exact regression that killed the last one —
// the deterministic half of verifier co-evolution, no LLM in the loop. The
// weak-case guard in RecordSkillValidationCase filters traces with no
// concrete tool evidence; that rejection is quiet and expected.
func (e *Evolver) distillRollbackValidationCase(skillName string) {
	stats, err := e.tracker.Stats(skillName)
	if err != nil || stats == nil || len(stats.RecentFailureTraces) == 0 {
		return
	}
	for i := len(stats.RecentFailureTraces) - 1; i >= 0; i-- { // newest evidence first
		tr := stats.RecentFailureTraces[i]
		if strings.TrimSpace(tr.ToolName) == "" {
			continue
		}
		call := SkillReplayToolCallRecord{Name: strings.TrimSpace(tr.ToolName)}
		if frag := strings.TrimSpace(tr.ToolInput); frag != "" {
			call.InputIncludes = []string{genesiscommon.TruncateRunes(frag, 120)}
		}
		desc := strings.TrimSpace(tr.AgentMechanism)
		if desc == "" {
			desc = strings.TrimSpace(tr.ErrorMsg)
		}
		rec := SkillValidationCaseRecord{
			SkillName:    skillName,
			Description:  genesiscommon.TruncateRunes("post-rollback regression evidence: "+desc, 400),
			FrontierTier: "hard",
			Source:       "post-rollback",
			Replay: SkillReplayCaseRecord{
				Input:             genesiscommon.TruncateRunes(strings.TrimSpace(tr.Signature), 200),
				ExpectedToolCalls: []SkillReplayToolCallRecord{call},
			},
		}
		if err := e.tracker.RecordSkillValidationCase(rec); err != nil {
			if !errors.Is(err, ErrWeakAutomaticValidationCase) {
				e.logger.Warn("evolver: rollback case distillation failed", "skill", skillName, "error", err)
			}
			continue
		}
		e.logger.Info("evolver: rollback evidence distilled into held-out case", "skill", skillName, "tool", call.Name)
		return // one case per rollback — the cap is the rollback cadence itself
	}
}
