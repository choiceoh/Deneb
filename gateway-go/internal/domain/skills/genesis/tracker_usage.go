package genesis

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Usage-record flow split out of tracker.go (pure move, no behavior change):
// ingest/record, rollback firing, and the stats/health query surface.

// ingest updates in-memory aggregates from a single usage record. Only real-use
// records (isRealUsageRecord) count toward the success-rate aggregate the
// evolver gates on — both live and on startup replay, which also discards the
// historical review/infra backlog that would otherwise re-pollute the rate on
// every restart and pin a healthy skill as a phantom underperformer.
func (t *Tracker) ingest(r UsageRecord) {
	if !isRealUsageRecord(r) {
		return
	}
	agg := t.stats[r.SkillName]
	if agg == nil {
		agg = &usageAgg{}
		t.stats[r.SkillName] = agg
	}
	agg.total++
	if r.Success {
		agg.success++
	} else {
		agg.failure++
	}
	if r.UsedAt > agg.lastUsed {
		agg.lastUsed = r.UsedAt
	}

	if !r.Success && r.ErrorMsg != "" {
		errs := t.recentErrors[r.SkillName]
		errs = append(errs, r.ErrorMsg)
		if len(errs) > 5 {
			errs = errs[len(errs)-5:]
		}
		t.recentErrors[r.SkillName] = errs
	}
	if trace := usageFailureTraceFromRecord(r); trace != nil {
		traces := t.recentFailureTraces[r.SkillName]
		traces = append(traces, *trace)
		if len(traces) > 5 {
			traces = traces[len(traces)-5:]
		}
		t.recentFailureTraces[r.SkillName] = traces
	}
}

// RecordUsage logs a skill usage event.
func (t *Tracker) RecordUsage(record UsageRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.UsedAt == 0 {
		record.UsedAt = time.Now().UnixMilli()
	}
	record = normalizeUsageFailureTrace(record)

	if err := jsonlstore.Append(t.usagePath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append usage: %w", err)
	}
	t.ingest(record)
	t.maybeFireRollbackLocked(record)
	// Curator use-count / staleness must reflect real use only (consistent with
	// the success rate): a review verdict isn't an execution, so it must not bump
	// lastUsed and mask a never-really-used skill from staleness pruning — that
	// masking is what lets dead self-generated skills linger as net-negative cost.
	if isRealUsageRecord(record) {
		if err := t.touchCuratorUsageLocked(record.SkillName, record.UsedAt); err != nil {
			return err
		}
	}
	return nil
}

// maybeFireRollbackLocked advances the post-evolve watch for the used skill and
// reverts the evolution when failures reach rollbackThreshold within the
// observation window. Windowed rather than strict-consecutive: a single success
// no longer clears the watch, so a regression that alternates pass/fail (the
// sub-threshold coupling HarnessX §6.6 documents) still rolls back. Caller holds
// t.mu; the callback runs lock-free in a recovered goroutine (it re-enters the
// tracker via LogEvolveRolledBack, so it must not run under the lock).
// loadWatchesLocked restores persisted in-flight watches. Caller holds no
// lock during NewTracker; named for the convention since it touches t state.
func (t *Tracker) loadWatchesLocked() {
	raw, err := os.ReadFile(t.watchPath)
	if err != nil {
		return // absent (fresh install) or unreadable — start clean
	}
	var persisted map[string]persistedEvolveWatch
	if err := json.Unmarshal(raw, &persisted); err != nil {
		if t.logger != nil {
			t.logger.Warn("genesis: evolve watch state corrupt, starting clean", "error", err)
		}
		return
	}
	for skill, w := range persisted {
		t.postEvolve[skill] = &evolveWatch{
			version:       w.Version,
			audit:         w.Audit,
			postUses:      w.PostUses,
			postFails:     w.PostFails,
			recurred:      w.Recurred,
			baselineUses:  w.BaselineUses,
			baselineFails: w.BaselineFails,
			ep:            w.EProcess,
			createdAt:     w.CreatedAt,
		}
		// Pre-timestamp watches (persisted by an older binary) start their
		// resolution clock at restore rather than never expiring.
		if t.postEvolve[skill].createdAt == 0 {
			t.postEvolve[skill].createdAt = time.Now().UnixMilli()
		}
	}
	if len(persisted) > 0 && t.logger != nil {
		t.logger.Info("genesis: restored in-flight evolve watches", "count", len(persisted))
	}
}

// saveWatchesLocked persists the in-flight watches (caller holds t.mu).
// Best-effort: a write failure is logged, never blocks the usage path.
func (t *Tracker) saveWatchesLocked() {
	if t.watchPath == "" {
		return
	}
	out := make(map[string]persistedEvolveWatch, len(t.postEvolve))
	for skill, w := range t.postEvolve {
		out[skill] = persistedEvolveWatch{
			Version:       w.version,
			Audit:         w.audit,
			PostUses:      w.postUses,
			PostFails:     w.postFails,
			Recurred:      w.recurred,
			BaselineUses:  w.baselineUses,
			BaselineFails: w.baselineFails,
			EProcess:      w.ep,
			CreatedAt:     w.createdAt,
		}
	}
	raw, err := json.Marshal(out)
	if err == nil {
		err = atomicfile.WriteFile(t.watchPath, raw, &atomicfile.Options{Perm: 0o600})
	}
	if err != nil && t.logger != nil {
		t.logger.Warn("genesis: evolve watch persist failed", "error", err)
	}
}

// stashBaselineTestLocked captures the observation-mode verdict at the moment
// a watch resolves (caller holds t.mu). rollbackFired distinguishes the two
// disagreement classes: threshold-fired-but-test-quiet (possible false
// rollback) vs test-rejected-but-threshold-confirmed (possible missed
// regression).
func (t *Tracker) stashBaselineTestLocked(skill string, w *evolveWatch, rollbackFired bool) {
	if w == nil || w.ep == nil {
		return
	}
	reject := w.ep.Reject()
	if t.pendingBaselineTest == nil { // struct-literal construction in tests
		t.pendingBaselineTest = make(map[string]*RollbackBaselineTest)
	}
	t.pendingBaselineTest[skill] = &RollbackBaselineTest{
		EValue:       w.ep.E,
		N:            w.ep.N,
		Reject:       reject,
		Baseline:     w.ep.Baseline,
		Disagreement: rollbackFired != reject,
	}
}

func (t *Tracker) maybeFireRollbackLocked(r UsageRecord) {
	w := t.postEvolve[r.SkillName]
	if w == nil || t.rollback == nil {
		return
	}
	// Only real use is fair evidence for or against a fresh evolve: a review-fork
	// record or a consult-infra failure must neither advance the failure count
	// (reverting a good evolve) nor count toward proving it out.
	if !isRealUsageRecord(r) {
		return
	}
	w.postUses++
	if !r.Success {
		w.postFails++
		if selfHarnessTargetRecurred(w.audit, r) {
			w.recurred++
		}
	}
	w.ep.Observe(!r.Success)
	t.saveWatchesLocked()
	// Regression: threshold failures within the window — fire the rollback.
	if w.postFails >= t.rollbackThreshold {
		t.stashBaselineTestLocked(r.SkillName, w, true)
		recurred := w.recurred
		threshold := t.rollbackThreshold // capture under the lock; the goroutine must not read t.* fields
		delete(t.postEvolve, r.SkillName)
		t.saveWatchesLocked()
		fn := t.rollback
		skill := r.SkillName
		go func() {
			defer func() {
				if rec := recover(); rec != nil && t.logger != nil {
					t.logger.Error("genesis: rollback callback panic", "panic", rec)
				}
			}()
			if t.logger != nil {
				t.logger.Info("genesis: post-evolve regression window tripped, rolling back",
					"skill", skill, "fails", threshold, "targetRecurrences", recurred)
			}
			fn(skill)
		}()
		return
	}
	// Proven out: the evolve survived the observation window below the failure
	// threshold. Close the loop with a positive confirmation (#1) — the watch
	// used to be dropped silently here, so a shipped evolve only ever produced a
	// rollback event, never an "it held up" event. Now every shipped evolve ends
	// in either evolve_rolled_back or evolve_confirmed.
	if w.postUses >= t.rollbackWindowLocked() {
		t.stashBaselineTestLocked(r.SkillName, w, false)
		uses, fails, recurred := w.postUses, w.postFails, w.recurred
		audit := w.audit
		skill := r.SkillName
		delete(t.postEvolve, r.SkillName)
		t.saveWatchesLocked()
		go func() {
			defer func() {
				if rec := recover(); rec != nil && t.logger != nil {
					t.logger.Error("genesis: confirm callback panic", "panic", rec)
				}
			}()
			t.confirmEvolve(skill, audit, uses, fails, recurred)
		}()
	}
}

// rollbackWindowLocked is the number of post-evolve real uses observed before an
// evolve that has not hit the failure threshold is considered proven and its
// watch cleared. Twice the threshold, so reaching the threshold means at least a
// ~50% failure rate over the window. Caller holds t.mu.
func (t *Tracker) rollbackWindowLocked() int {
	if t.rollbackThreshold <= 0 {
		return 0
	}
	return t.rollbackThreshold * 2
}

// ResolveStaleWatches closes rollback watches older than maxAge that the
// usage-driven path will never resolve (backtest 2026-07-11: zero historical
// resolutions — post-evolve real usage runs out at 0-5 uses, below the 6-use
// window). A stale watch with at least one clean-enough use confirms with a
// small-sample marker (its observation-mode e-process verdict still rides the
// entry — this is what unblocks the P3 label pipeline); a stale watch with
// ZERO uses expires without polluting confirm/rollback statistics. Returns
// the number of watches closed.
func (t *Tracker) ResolveStaleWatches(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	type confirmJob struct {
		skill                 string
		audit                 HarnessEditAudit
		uses, fails, recurred int
	}
	var confirms []confirmJob
	var expired []string

	t.mu.Lock()
	now := time.Now().UnixMilli()
	for skill, w := range t.postEvolve {
		if w.createdAt == 0 || now-w.createdAt < maxAge.Milliseconds() {
			continue
		}
		if w.postUses == 0 {
			expired = append(expired, skill)
			delete(t.postEvolve, skill)
			continue
		}
		// A watch at/over the failure threshold cannot be stale (the fire path
		// resolves it on the use that crossed it) — guard anyway.
		if t.rollbackThreshold > 0 && w.postFails >= t.rollbackThreshold {
			continue
		}
		t.stashBaselineTestLocked(skill, w, false)
		confirms = append(confirms, confirmJob{skill, w.audit, w.postUses, w.postFails, w.recurred})
		delete(t.postEvolve, skill)
	}
	if len(confirms)+len(expired) > 0 {
		t.saveWatchesLocked()
	}
	t.mu.Unlock()

	for _, skill := range expired {
		if t.logger != nil {
			t.logger.Info("genesis: post-evolve watch expired unused (no real usage within window)",
				"skill", skill, "maxAge", maxAge)
		}
		if err := t.LogEvolveWatchExpired(skill); err != nil && t.logger != nil {
			t.logger.Warn("genesis: watch-expired lifecycle log failed", "skill", skill, "error", err)
		}
	}
	for _, c := range confirms {
		if t.logger != nil {
			t.logger.Info("genesis: post-evolve watch resolved time-based (window unfilled)",
				"skill", c.skill, "uses", c.uses, "fails", c.fails, "maxAge", maxAge)
		}
		t.confirmEvolve(c.skill, c.audit, c.uses, c.fails, c.recurred)
	}
	return len(confirms) + len(expired)
}

func selfHarnessTargetRecurred(audit HarnessEditAudit, r UsageRecord) bool {
	target := normalizedSelfHarnessSignature(audit.TargetSignature)
	if target == "" || r.Success || !isRealUsageRecord(r) {
		return false
	}
	trace := usageFailureTraceFromRecord(r)
	if trace == nil {
		return false
	}
	return selfHarnessSignatureMatches(target, normalizedSelfHarnessSignature(trace.Signature))
}

// Stats returns aggregated usage stats for a skill.
func (t *Tracker) Stats(skillName string) (*UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.getStatsLocked(skillName), nil
}

func (t *Tracker) getStatsLocked(skillName string) *UsageStats {
	stats := &UsageStats{SkillName: skillName}
	agg := t.stats[skillName]
	if agg == nil {
		return stats
	}
	stats.TotalUses = agg.total
	stats.SuccessCount = agg.success
	stats.FailureCount = agg.failure
	stats.LastUsed = agg.lastUsed
	if agg.total > 0 {
		stats.SuccessRate = float64(agg.success) / float64(agg.total)
	}
	if errs := t.recentErrors[skillName]; len(errs) > 0 {
		stats.RecentErrors = make([]string, len(errs))
		copy(stats.RecentErrors, errs)
	}
	if traces := t.recentFailureTraces[skillName]; len(traces) > 0 {
		stats.RecentFailureTraces = make([]UsageFailureTrace, len(traces))
		copy(stats.RecentFailureTraces, traces)
	}
	return stats
}

// EvolutionEvidenceStats returns the bounded usage evidence that the automatic
// evolver is allowed to act on. It intentionally differs from Stats(), which is
// a lifetime observability aggregate: stale failures should remain visible in
// status output, but they must not keep triggering fresh rewrites forever.
func (t *Tracker) EvolutionEvidenceStats(skillName string) (*UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	statsBySkill, err := t.evolutionEvidenceStatsBySkillLocked(time.Now())
	if err != nil {
		return nil, err
	}
	if stats := statsBySkill[skillName]; stats != nil {
		return stats, nil
	}
	return &UsageStats{SkillName: skillName}, nil
}

// ListAllStats returns usage stats for all tracked skills.
func (t *Tracker) ListAllStats() ([]UsageStats, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make([]UsageStats, 0, len(t.stats))
	for name := range t.stats {
		result = append(result, *t.getStatsLocked(name))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalUses > result[j].TotalUses
	})
	return result, nil
}

// Evolve-health window + thrash thresholds. A single skill eating most of a
// non-trivial recent evolve budget is the thrash signature (email-analysis ran
// 6 evolves in ~2 days, all one skill, undetected — PR #2328).
const (
	evolutionHealthWindow   = 7 * 24 * time.Hour
	evolutionHealthCacheTTL = 60 * time.Second
	evolutionThrashCooldown = 24 * time.Hour
	// Thrash = one skill re-evolved >= evolutionThrashMinEvolves times in the
	// window AND accounting for >= evolutionThrashDominancePct of all evolves. A
	// good evolve should stick, so a skill needing 3+ fixes in a week while
	// dominating the budget is the non-convergence signature (email-analysis hit
	// 6). Tuned to flag early; a false positive only shows an operator a glance.
	evolutionThrashMinEvolves   = 3
	evolutionThrashDominancePct = 60
)

// EvolutionHealthSummary surfaces evolve-loop productivity for /health so a
// silent thrash (the loop burning its budget re-evolving one skill) is visible
// without log spelunking — the failure mode behind every past silent death.
type EvolutionHealthSummary struct {
	Evolves7d               int     `json:"evolves7d"`
	EvolveRejected7d        int     `json:"evolveRejected7d"`
	EvolveRolledBack7d      int     `json:"evolveRolledBack7d"`
	EvolveConfirmed7d       int     `json:"evolveConfirmed7d"`
	CrossSkillRegressions7d int     `json:"crossSkillRegressions7d"`
	ConfirmRate             float64 `json:"confirmRate"`
	// FalseAcceptRate is the judge's observed false-accept rate over RESOLVED
	// evolves (rolledBack/(confirmed+rolledBack)) with ResolvedEvolves7d as
	// the sample size — the first observation window on a judge quietly going
	// soft (RSI P1.5 ⑤, CoVerRL). Complement of ConfirmRate, made explicit so
	// the scoreboard reads without arithmetic; n alongside so a 1/1 doesn't
	// masquerade as certainty.
	FalseAcceptRate         float64 `json:"falseAcceptRate"`
	ResolvedEvolves7d       int     `json:"resolvedEvolves7d"`
	Genesis7d               int     `json:"genesis7d"`
	DistinctSkillsEvolved7d int     `json:"distinctSkillsEvolved7d"`
	TopEvolvedSkill         string  `json:"topEvolvedSkill,omitempty"`
	TopEvolvedCount         int     `json:"topEvolvedCount,omitempty"`
	LastRejectedSkill       string  `json:"lastRejectedSkill,omitempty"`
	LastRejectedReason      string  `json:"lastRejectedReason,omitempty"`
	Thrash                  bool    `json:"thrash"`
	ThrashCooldownUntil     int64   `json:"thrashCooldownUntil,omitempty"`
}

// EvolutionHealth summarizes evolve/genesis activity over the last 7 days from
// the persisted lifecycle log (so the counts survive the frequent SIGUSR1
// restarts). Cached for evolutionHealthCacheTTL to bound rescans of the growing
// log under frequent /health polls.
func (t *Tracker) EvolutionHealth() EvolutionHealthSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if !t.evoHealthAt.IsZero() && now.Sub(t.evoHealthAt) < evolutionHealthCacheTTL {
		return t.evoHealth
	}
	t.evoHealth = t.computeEvolutionHealthLocked(now)
	t.evoHealthAt = now
	return t.evoHealth
}

func (t *Tracker) computeEvolutionHealthLocked(now time.Time) EvolutionHealthSummary {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return EvolutionHealthSummary{}
	}
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	perSkill := map[string]int{}
	var s EvolutionHealthSummary
	for _, e := range entries {
		if e.CreatedAt < cutoff {
			continue
		}
		switch e.Type {
		case "evolved":
			s.Evolves7d++
			if e.SkillName != "" {
				perSkill[e.SkillName]++
			}
		case "evolve_rejected":
			s.EvolveRejected7d++
			if s.LastRejectedSkill == "" {
				s.LastRejectedSkill = e.SkillName
				s.LastRejectedReason = e.Reason
			}
		case "evolve_rolled_back":
			s.EvolveRolledBack7d++
		case "evolve_confirmed":
			s.EvolveConfirmed7d++
		case "cross_skill_regression":
			s.CrossSkillRegressions7d++
		case "genesis", "": // legacy genesis entries have no Type
			s.Genesis7d++
		}
	}
	s.DistinctSkillsEvolved7d = len(perSkill)
	if resolved := s.EvolveConfirmed7d + s.EvolveRolledBack7d; resolved > 0 {
		s.ResolvedEvolves7d = resolved
		s.FalseAcceptRate = float64(s.EvolveRolledBack7d) / float64(resolved)
		// Confirm rate = of the evolves that resolved (held up vs reverted), the
		// fraction that confirmed — the headline success metric of the loop, now
		// visible on /health instead of only living in the lifecycle log.
		s.ConfirmRate = float64(s.EvolveConfirmed7d) / float64(resolved)
	}
	for name, n := range perSkill {
		if n > s.TopEvolvedCount {
			s.TopEvolvedCount, s.TopEvolvedSkill = n, name
		}
	}
	if s.TopEvolvedCount >= evolutionThrashMinEvolves &&
		s.TopEvolvedCount*100 >= s.Evolves7d*evolutionThrashDominancePct {
		s.Thrash = true
		for _, e := range entries {
			if e.CreatedAt < cutoff || e.Type != "evolved" || e.SkillName != s.TopEvolvedSkill {
				continue
			}
			cooldownUntil := time.UnixMilli(e.CreatedAt).Add(evolutionThrashCooldown).UnixMilli()
			if cooldownUntil > s.ThrashCooldownUntil {
				s.ThrashCooldownUntil = cooldownUntil
			}
		}
	}
	return s
}

// AgentSkillValueSummary counts agent-created (genesis) skills and how many have
// zero real uses. Self-generated skills are net-negative unless they earn their
// keep (SoK -1.3pp); an unused one is pure cost (catalog + prompt tokens with no
// payoff). Surfaced on /health so dead weight is visible without inspecting the
// curator file. Real-use only: the curator use-count is now gated to genuine
// executions, so a verdict-only skill correctly reads as unused.
func (t *Tracker) AgentSkillValueSummary() (total, unused int) {
	report, err := t.SkillCuratorReport("")
	if err != nil {
		return 0, 0
	}
	for _, r := range report {
		if r.CreatedBy != SkillCuratorCreatedByAgent {
			continue
		}
		total++
		if r.UseCount == 0 {
			unused++
		}
	}
	return total, unused
}
