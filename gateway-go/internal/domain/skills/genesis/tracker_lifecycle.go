package genesis

import (
	"fmt"
	"sort"
	"time"

	genesiseprocess "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/eprocess"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Lifecycle log split out of tracker.go (pure move, no behavior change):
// genesis/evolve/rollback log entries, their writers, and the evidence queries
// (SkillsNeedingEvolution) built on them.

// EProcess keeps the tracker wire types stable while the implementation lives
// in the dependency-free eprocess child package.
type EProcess = genesiseprocess.EProcess

// LifecycleLogEntry is the combined JSONL view for genesis and evolution
// proposal events. Older genesis entries may not have Type populated; readers
// normalize those to "genesis".
type LifecycleLogEntry struct {
	Type             string                `json:"type,omitempty"`
	SkillName        string                `json:"skillName,omitempty"`
	Source           string                `json:"source,omitempty"`
	SessionKey       string                `json:"sessionKey,omitempty"`
	CreatedAt        int64                 `json:"createdAt,omitempty"`
	Category         string                `json:"category,omitempty"`
	Description      string                `json:"description,omitempty"`
	Candidate        string                `json:"candidate,omitempty"`
	Route            string                `json:"route,omitempty"`
	Evidence         string                `json:"evidence,omitempty"`
	Reason           string                `json:"reason,omitempty"`
	Executed         bool                  `json:"executed,omitempty"`
	Result           string                `json:"result,omitempty"`
	NewVersion       string                `json:"newVersion,omitempty"`
	SelfHarnessAudit *HarnessEditAudit     `json:"selfHarnessAudit,omitempty"`
	Provenance       *evolveProvenance     `json:"provenance,omitempty"`
	BaselineTest     *rollbackBaselineTest `json:"baselineTest,omitempty"`
}

// genesisLogEntry is the JSONL format for genesis log events.
type genesisLogEntry struct {
	Type        string `json:"type"`
	SkillName   string `json:"skillName"`
	Source      string `json:"source"`
	SessionKey  string `json:"sessionKey,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}

// EvolutionProposalRecord records an agent decision about whether recent
// experience should become a new skill, evolve an existing skill, or be skipped.
type EvolutionProposalRecord struct {
	Type       string `json:"type"`
	Candidate  string `json:"candidate"`
	Route      string `json:"route"`
	SessionKey string `json:"sessionKey,omitempty"`
	SkillName  string `json:"skillName,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Executed   bool   `json:"executed,omitempty"`
	Result     string `json:"result,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

// LogGenesis records that a skill was auto-generated.
func (t *Tracker) LogGenesis(skillName, source, sessionKey, category, description string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	createdAt := time.Now().UnixMilli()
	if err := jsonlstore.Append(t.logPath, genesisLogEntry{
		Type:        "genesis",
		SkillName:   skillName,
		Source:      source,
		SessionKey:  sessionKey,
		CreatedAt:   createdAt,
		Category:    category,
		Description: description,
	}); err != nil {
		return err
	}
	t.recordEvolutionActivityLocked(skillActivityGenesis, true, "")
	t.maybeFireEvolveLocked()
	return t.markSkillAgentCreatedLocked(skillName, createdAt)
}

// EvolveAttemptedSince reports whether skillName saw a committed evolve, a
// rollback, or an executed evolve-route proposal at/after sinceMs — the
// evolve-backlog reconciler's consumption evidence (an attempt, even a
// rejected or rolled-back one, means the opportunity was consumed).
func (t *Tracker) EvolveAttemptedSince(skillName string, sinceMs int64) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return false, fmt.Errorf("genesis-tracker: load lifecycle log: %w", err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.CreatedAt < sinceMs {
			break // append-ordered file — everything earlier is older
		}
		if e.SkillName != skillName {
			continue
		}
		switch e.Type {
		case "evolved", "evolve_rolled_back":
			return true, nil
		case "evolution_proposal":
			if e.Route == "evolve" && e.Executed {
				return true, nil
			}
		}
	}
	return false, nil
}

// LogEvolutionProposal records a Propus routing decision.
func (t *Tracker) LogEvolutionProposal(record EvolutionProposalRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.Type == "" {
		record.Type = "evolution_proposal"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if err := jsonlstore.Append(t.logPath, record); err != nil {
		return err
	}
	return nil
}

// evolveLogEntry is the JSONL format for evolve outcome events. Unlike the
// curator's MarkSkillPatched (which only tracks agent-created skills), this
// records every committed or rejected evolve — including ones on user-authored
// skills — so the native client can render a complete evolution timeline.
// evolveProvenance attributes an evolve decision to the exact evaluator
// configuration that produced it (RSI P1.5 certificate ledger): the effective
// meta-artifact versions, the judge identity, and the decision scores.
// Additive JSONL — older entries simply lack it; P2/P3 consume it as their
// label substrate (per-version judge accuracy, false-accept attribution).
type evolveProvenance struct {
	EvolveArtifactVersion string   `json:"evolveArtifactVersion,omitempty"`
	JudgeArtifactVersion  string   `json:"judgeArtifactVersion,omitempty"`
	JudgeModel            string   `json:"judgeModel,omitempty"`
	JudgeScoreOriginal    *float64 `json:"judgeScoreOriginal,omitempty"`
	JudgeScoreCandidate   *float64 `json:"judgeScoreCandidate,omitempty"`
	HeldOutMargin         *float64 `json:"heldOutMargin,omitempty"`
	// JudgeSwapConsistent records the order-swap consistency probe outcome for
	// an accepting forward verdict (RSI P3): true = the reversed pair was
	// rejected as required; false = the judge blessed both orders and the
	// evolve was refused. Absent when the probe was disabled or never reached.
	JudgeSwapConsistent *bool `json:"judgeSwapConsistent,omitempty"`
}

// rollbackBaselineTest is the baseline-aware test's verdict at the moment a
// watch resolved (observation mode): Disagreement marks the mislabeling class
// PACE warns about — on a rollback, the legacy threshold fired while the
// anytime-valid test had NOT rejected (possible false rollback); on a
// confirm, the test HAD rejected while the threshold confirmed (possible
// missed regression). These labels accumulate on the lifecycle ledger until
// there is enough evidence to switch the firing decision over.
type rollbackBaselineTest struct {
	EValue   float64 `json:"eValue"`
	N        int     `json:"n"`
	Reject   bool    `json:"reject"`
	Baseline float64 `json:"baseline"`
	// RejectReachable marks a fair comparison: the e-process observed at
	// least MinRejectObservations, so "did not reject" is a verdict rather
	// than a mathematical certainty. Legacy labels (absent field) read
	// false and are excluded from cutover readiness — they were recorded
	// while the confirm window made rejection unreachable (RSI eval C1).
	RejectReachable bool `json:"rejectReachable"`
	Disagreement    bool `json:"disagreement"`
}

type evolveLogEntry struct {
	Type             string                `json:"type"` // "evolved" | "evolve_rejected" | "evolve_rolled_back" | "evolve_confirmed" | "cross_skill_regression"
	SkillName        string                `json:"skillName"`
	NewVersion       string                `json:"newVersion,omitempty"`
	Description      string                `json:"description,omitempty"`
	Reason           string                `json:"reason,omitempty"`
	CreatedAt        int64                 `json:"createdAt"`
	SelfHarnessAudit *HarnessEditAudit     `json:"selfHarnessAudit,omitempty"`
	Provenance       *evolveProvenance     `json:"provenance,omitempty"`
	BaselineTest     *rollbackBaselineTest `json:"baselineTest,omitempty"`
}

// LogEvolve records a committed skill evolution (rewrite applied to disk) and
// starts the post-evolve rollback watch so the next few uses are monitored.
func (t *Tracker) LogEvolve(skillName, newVersion, description string) error {
	return t.LogEvolveWithAudit(skillName, newVersion, description, HarnessEditAudit{})
}

// LogEvolveWithAudit records a committed skill evolution with structured
// Self-Harness transition metadata.
func (t *Tracker) LogEvolveWithAudit(skillName, newVersion, description string, audit HarnessEditAudit) error {
	return t.logEvolveWithProvenance(skillName, newVersion, description, audit, nil)
}

// logEvolveWithProvenance is LogEvolveWithAudit plus the evaluator-attribution
// certificate (RSI P1.5). prov may be nil (legacy callers).
func (t *Tracker) logEvolveWithProvenance(skillName, newVersion, description string, audit HarnessEditAudit, prov *evolveProvenance) error {
	audit = withHarnessDimensions(audit)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	if t.rollbackThreshold > 0 {
		w := &evolveWatch{version: newVersion, audit: audit, createdAt: now}
		// Pre-evolve baseline snapshot (PACE): what "healthy" looked like
		// before this evolve, for the future baseline-aware rollback test.
		if s := t.getStatsLocked(skillName); s != nil {
			w.baselineUses = s.TotalUses
			w.baselineFails = s.FailureCount
		}
		baselineRate := 0.0
		if w.baselineUses > 0 {
			baselineRate = float64(w.baselineFails) / float64(w.baselineUses)
		}
		w.ep = genesiseprocess.NewEProcess(genesiseprocess.DefaultEProcessAlpha, baselineRate)
		t.postEvolve[skillName] = w
		t.saveWatchesLocked()
	}
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolved",
		SkillName:        skillName,
		NewVersion:       newVersion,
		Description:      description,
		CreatedAt:        now,
		SelfHarnessAudit: audit.Ptr(),
		Provenance:       prov,
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "accepted", description, now)
	return nil
}

// logEvolveRolledBack records that an evolution was reverted after it regressed
// (rollbackThreshold post-evolve failures within the observation window).
func (t *Tracker) logEvolveRolledBack(skillName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	bt := t.pendingBaselineTest[skillName]
	delete(t.pendingBaselineTest, skillName)
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:         "evolve_rolled_back",
		SkillName:    skillName,
		Reason:       evolveRollbackReason,
		CreatedAt:    now,
		BaselineTest: bt,
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "rolled_back", evolveRollbackReason, now)
	return nil
}

// confirmEvolve records that an evolve survived its post-evolve observation
// window below the rollback threshold (#1 falsification closure). It runs
// lock-free off the tracker (LogEvolveConfirmed re-enters t.mu). A clean
// confirmation means the target failure signature never recurred and the skill
// logged no failures within the window; otherwise it is partial (held up
// overall, but the targeted mechanism still surfaced below threshold).
func (t *Tracker) confirmEvolve(skillName string, audit HarnessEditAudit, uses, fails, recurred int) {
	clean := fails == 0 && recurred == 0
	if t.logger != nil {
		t.logger.Info("genesis: post-evolve window cleared, evolve confirmed",
			"skill", skillName, "uses", uses, "fails", fails, "targetRecurrences", recurred,
			"clean", clean, "expectedBehaviorChange", audit.ExpectedBehaviorChange)
	}
	if err := t.logEvolveConfirmed(skillName, audit, clean); err != nil && t.logger != nil {
		t.logger.Warn("genesis: confirm lifecycle log failed", "skill", skillName, "error", err)
	}
}

// logEvolveConfirmed records that an evolution proved out over its post-evolve
// observation window (#1). It is the positive counterpart to LogEvolveRolledBack,
// so the lifecycle log now carries the outcome of every shipped evolve. clean
// reports whether the target failure signature never recurred and no failures
// occurred within the window.
func (t *Tracker) logEvolveConfirmed(skillName string, audit HarnessEditAudit, clean bool) error {
	audit = withHarnessDimensions(audit)
	t.mu.Lock()
	defer t.mu.Unlock()
	reason := "partial: held up but target signature recurred below threshold"
	if clean {
		reason = "clean: target signature did not recur within the window"
	}
	bt := t.pendingBaselineTest[skillName]
	delete(t.pendingBaselineTest, skillName)
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolve_confirmed",
		SkillName:        skillName,
		Reason:           reason,
		CreatedAt:        time.Now().UnixMilli(),
		BaselineTest:     bt,
		SelfHarnessAudit: audit.Ptr(),
	})
}

// handleFailedRollback records that a fired rollback did NOT restore the
// backup (missing backup or write error) and drops the stashed baseline
// label so it cannot attach to a later resolution of the same skill (RSI
// code eval H3). A distinct lifecycle type — NOT evolve_rolled_back — because
// the regressing body is still live; conflating it with a real rollback would
// overstate the loop's self-correction rate and hide a stuck skill. Logged at
// Error: the skill is regressing in real use and the automatic revert failed,
// which is an operator-visible failure.
func (t *Tracker) handleFailedRollback(skillName string) {
	t.mu.Lock()
	bt := t.pendingBaselineTest[skillName]
	delete(t.pendingBaselineTest, skillName)
	err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:         "evolve_rollback_failed",
		SkillName:    skillName,
		Reason:       "rollback fired but backup restore failed — regressing body still live",
		CreatedAt:    time.Now().UnixMilli(),
		BaselineTest: bt,
	})
	t.mu.Unlock()
	if t.logger != nil {
		if err != nil {
			t.logger.Error("genesis: failed-rollback lifecycle log write failed", "skill", skillName, "error", err)
		} else {
			t.logger.Error("genesis: rollback fired but restore failed — regressing skill still live", "skill", skillName)
		}
	}
}

// logEvolveWatchExpired records a watch that aged out with ZERO post-evolve
// real uses — closed for hygiene, but deliberately a distinct type so it never
// counts as confirmed (no evidence) nor rolled back in health statistics.
func (t *Tracker) logEvolveWatchExpired(skillName string) error {
	// Hold t.mu like every other lifecycle writer in this file: the mutex
	// serializes JSONL appends to t.logPath so a concurrent writer cannot
	// interleave a partial record (reviewer feedback, #3460). Callers
	// (ResolveStaleWatches) release the lock before invoking this.
	t.mu.Lock()
	defer t.mu.Unlock()
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:      "evolve_watch_expired",
		SkillName: skillName,
		Reason:    "watch aged out with zero real uses",
		CreatedAt: time.Now().UnixMilli(),
	})
}

// LogEvolveRejected records an evolve attempt whose rewrite the self-test
// refused to commit (the original skill was kept).
func (t *Tracker) LogEvolveRejected(skillName, reason string) error {
	return t.LogEvolveRejectedWithAudit(skillName, reason, HarnessEditAudit{})
}

// LogEvolveRejectedWithAudit records a rejected skill evolution with structured
// Self-Harness transition metadata from the candidate that failed validation.
func (t *Tracker) LogEvolveRejectedWithAudit(skillName, reason string, audit HarnessEditAudit) error {
	return t.logEvolveRejectedWithProvenance(skillName, reason, audit, nil)
}

// logEvolveRejectedWithProvenance is LogEvolveRejectedWithAudit plus the
// evaluator-attribution certificate (RSI P1.5). prov may be nil.
func (t *Tracker) logEvolveRejectedWithProvenance(skillName, reason string, audit HarnessEditAudit, prov *evolveProvenance) error {
	audit = withHarnessDimensions(audit)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UnixMilli()
	if err := jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:             "evolve_rejected",
		SkillName:        skillName,
		Reason:           reason,
		CreatedAt:        now,
		SelfHarnessAudit: audit.Ptr(),
		Provenance:       prov,
	}); err != nil {
		return err
	}
	t.recordOptimizerMemoryLocked(skillName, "rejected", reason, now)
	return nil
}

// logCrossSkillRegression records that a committed evolve of sourceSkill made a
// similar NEIGHBOR skill regress the evolved skill's held-out forbidden/required
// assertions (#4 cross-skill regression detection). It is an observation only:
// the evolve is NOT rolled back — a shared-tag/description neighbor failing the
// evolved skill's contract is a coupling signal worth surfacing, not proof the
// edit is wrong (the neighbor was never under that contract). neighborSkill is
// the skill that regressed; sourceSkill is the evolve that triggered the check.
func (t *Tracker) logCrossSkillRegression(sourceSkill, neighborSkill, reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:        "cross_skill_regression",
		SkillName:   neighborSkill,
		Description: "evolve of " + sourceSkill + " surfaced a regression in neighbor " + neighborSkill,
		Reason:      reason,
		CreatedAt:   time.Now().UnixMilli(),
	})
}

// logGateExploit records that a synthetic exploit-shaped candidate PASSED the
// deterministic preflight (adversarial gate-trap probe, 2605.20744) — a
// gate-integrity alarm, never an accepted edit (the trap is never committed).
func (t *Tracker) logGateExploit(skillName, reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type:      "gate_exploit",
		SkillName: skillName,
		Reason:    reason,
		CreatedAt: time.Now().UnixMilli(),
	})
}

// RecentLifecycleLog returns recent genesis/proposal events, newest first.
func (t *Tracker) RecentLifecycleLog(limit int) ([]LifecycleLogEntry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load lifecycle log: %w", err)
	}
	for i := range entries {
		if entries[i].Type == "" {
			entries[i].Type = "genesis"
		}
		if entries[i].SelfHarnessAudit != nil {
			audit := withHarnessDimensions(*entries[i].SelfHarnessAudit)
			entries[i].SelfHarnessAudit = audit.Ptr()
		}
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// skillsNeedingEvolution returns skills with recent unresolved failure rates.
func (t *Tracker) skillsNeedingEvolution(minUses int, maxSuccessRate float64) ([]UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	statsBySkill, err := t.evolutionEvidenceStatsBySkillLocked(time.Now())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	evoHealth := t.computeEvolutionHealthLocked(now)

	var candidates []UsageStats
	for _, stats := range statsBySkill {
		s := *stats
		if s.TotalUses < minUses || s.FailureCount == 0 || s.SuccessRate > maxSuccessRate {
			continue
		}
		if evoHealth.Thrash &&
			s.SkillName == evoHealth.TopEvolvedSkill &&
			evoHealth.ThrashCooldownUntil > now.UnixMilli() {
			continue
		}
		candidates = append(candidates, s)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TotalUses > candidates[j].TotalUses
	})
	return candidates, nil
}

func (t *Tracker) evolutionEvidenceStatsBySkillLocked(now time.Time) (map[string]*UsageStats, error) {
	lastAttemptAt, err := t.lastEvolutionAttemptBySkillLocked()
	if err != nil {
		return nil, err
	}
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load usage for evolution evidence: %w", err)
	}

	statsBySkill := make(map[string]*UsageStats)
	for _, r := range records {
		if r.SkillName == "" || !isRealUsageRecord(r) {
			continue
		}
		if cutoff := evolutionEvidenceCutoff(now, lastAttemptAt[r.SkillName]); cutoff > 0 && r.UsedAt <= cutoff {
			continue
		}
		stats := statsBySkill[r.SkillName]
		if stats == nil {
			stats = &UsageStats{SkillName: r.SkillName}
			statsBySkill[r.SkillName] = stats
		}
		addUsageRecordToStats(stats, r)
	}
	for _, stats := range statsBySkill {
		if stats.TotalUses > 0 {
			stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalUses)
		}
	}
	return statsBySkill, nil
}

func evolutionEvidenceCutoff(now time.Time, lastAttemptAt int64) int64 {
	cutoff := lastAttemptAt
	if window := skillEvolutionEvidenceWindow(); window > 0 {
		windowCutoff := now.Add(-window).UnixMilli()
		if windowCutoff > cutoff {
			cutoff = windowCutoff
		}
	}
	return cutoff
}

func skillEvolutionEvidenceWindow() time.Duration {
	days := envInt("DENEB_SKILL_EVOLVE_EVIDENCE_DAYS", defaultSkillEvolutionEvidenceWindowDays)
	if days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

func addUsageRecordToStats(stats *UsageStats, r UsageRecord) {
	stats.TotalUses++
	if r.Success {
		stats.SuccessCount++
	} else {
		stats.FailureCount++
	}
	if r.UsedAt > stats.LastUsed {
		stats.LastUsed = r.UsedAt
	}
	if !r.Success && r.ErrorMsg != "" {
		stats.RecentErrors = append(stats.RecentErrors, r.ErrorMsg)
		if len(stats.RecentErrors) > 5 {
			stats.RecentErrors = stats.RecentErrors[len(stats.RecentErrors)-5:]
		}
	}
	if trace := usageFailureTraceFromRecord(r); trace != nil {
		stats.RecentFailureTraces = append(stats.RecentFailureTraces, *trace)
		if len(stats.RecentFailureTraces) > 5 {
			stats.RecentFailureTraces = stats.RecentFailureTraces[len(stats.RecentFailureTraces)-5:]
		}
	}
}

func (t *Tracker) lastEvolutionAttemptBySkillLocked() (map[string]int64, error) {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load lifecycle log for evolution candidates: %w", err)
	}
	out := make(map[string]int64)
	for _, entry := range entries {
		if entry.SkillName == "" || !isEvolutionAttemptType(entry.Type) {
			continue
		}
		if entry.CreatedAt > out[entry.SkillName] {
			out[entry.SkillName] = entry.CreatedAt
		}
	}
	return out, nil
}

func isEvolutionAttemptType(typ string) bool {
	switch typ {
	case "evolved", "evolve_rejected", "evolve_rolled_back":
		return true
	default:
		return false
	}
}
