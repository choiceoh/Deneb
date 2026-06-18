package genesis

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	return &Tracker{
		logger:              slog.Default(),
		usagePath:           filepath.Join(dir, "skill_usage.jsonl"),
		logPath:             filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:         filepath.Join(dir, "skill_curator_state.json"),
		livenessPath:        filepath.Join(dir, "skill_liveness.json"),
		rejectedPath:        filepath.Join(dir, "skill_rejected_edits.jsonl"),
		opportunityPath:     filepath.Join(dir, "skill_opportunities.jsonl"),
		optimizerMemoryPath: filepath.Join(dir, "skill_optimizer_memory.json"),
		validationPath:      filepath.Join(dir, "skill_validation_cases.jsonl"),
		selfCorrectionPath:  filepath.Join(dir, "self_correction_candidates.jsonl"),
		stats:               make(map[string]*usageAgg),
		recentErrors:        make(map[string][]string),
		postEvolve:          make(map[string]*evolveWatch),
	}
}

// TestPostEvolveRollback_FiresAfterConsecutiveFailures verifies the post-evolve
// watch reverts an evolution after the configured number of consecutive
// failures, and only then.
func TestPostEvolveRollback_FiresAfterConsecutiveFailures(t *testing.T) {
	tr := newTestTracker(t)
	fired := make(chan string, 1)
	tr.SetRollback(func(s string) { fired <- s }, 3)

	if err := tr.LogEvolve("deploy-helper", "1.0.1", "tighten steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	fail := func() {
		if err := tr.RecordUsage(UsageRecord{SkillName: "deploy-helper", Success: false, ErrorMsg: "boom"}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	fail() // 1
	fail() // 2
	select {
	case <-fired:
		t.Fatal("rollback fired before reaching the failure threshold")
	default:
	}
	fail() // 3 → revert

	select {
	case got := <-fired:
		if got != "deploy-helper" {
			t.Fatalf("rollback fired for %q, want deploy-helper", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rollback did not fire after 3 consecutive post-evolve failures")
	}
}

// TestPostEvolveRollback_SuccessClearsWatch verifies a single success between
// failures validates the evolution and stops the watch, so later failures of an
// already-accepted skill do not trigger a spurious rollback.
func TestPostEvolveRollback_SuccessClearsWatch(t *testing.T) {
	tr := newTestTracker(t)
	fired := make(chan string, 1)
	tr.SetRollback(func(s string) { fired <- s }, 3)

	if err := tr.LogEvolve("deploy-helper", "1.0.1", "tighten steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	rec := func(ok bool) {
		if err := tr.RecordUsage(UsageRecord{SkillName: "deploy-helper", Success: ok}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	rec(false) // 1 fail
	rec(false) // 2 fail
	rec(true)  // success → watch cleared
	rec(false) // these can't accumulate to a rollback anymore
	rec(false)
	rec(false)

	select {
	case got := <-fired:
		t.Fatalf("rollback fired (%q) after a success cleared the watch", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestTrackerRecentLifecycleLog_NewestFirstAndTyped(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	if err := tracker.LogEvolutionProposal(EvolutionProposalRecord{
		Candidate: "repeatable deploy fix",
		Route:     "evolve",
		SkillName: "deploy-helper",
		Executed:  true,
	}); err != nil {
		t.Fatalf("LogEvolutionProposal: %v", err)
	}

	entries, err := tracker.RecentLifecycleLog(10)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 lifecycle entries, got %d", len(entries))
	}
	if entries[0].Type != "evolution_proposal" || entries[0].Candidate != "repeatable deploy fix" {
		t.Fatalf("expected newest proposal first, got %+v", entries[0])
	}
	if entries[1].Type != "genesis" || entries[1].SkillName != "deploy-helper" {
		t.Fatalf("expected typed genesis entry second, got %+v", entries[1])
	}
}

func TestTrackerLifecycleLogStoresSelfHarnessAudit(t *testing.T) {
	tracker := newTestTracker(t)
	audit := HarnessEditAudit{
		TargetSignature:        "terminal=timeout|mechanism=bounded-execution",
		EditedSurface:          "Procedure",
		ExpectedBehaviorChange: "pivot after a timeout",
		RegressionRisk:         "do not skip final verification",
	}
	if err := tracker.LogEvolveWithAudit("deploy-helper", "1.0.1", "tighten timeout recovery", audit); err != nil {
		t.Fatalf("LogEvolveWithAudit: %v", err)
	}
	if err := tracker.LogEvolveRejectedWithAudit("deploy-helper", "judge rejected candidate", audit); err != nil {
		t.Fatalf("LogEvolveRejectedWithAudit: %v", err)
	}

	entries, err := tracker.RecentLifecycleLog(2)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	for _, entry := range entries {
		if entry.SelfHarnessAudit == nil {
			t.Fatalf("expected audit on lifecycle entry, got %+v", entry)
		}
		if entry.SelfHarnessAudit.TargetSignature != audit.TargetSignature ||
			entry.SelfHarnessAudit.EditedSurface != audit.EditedSurface ||
			entry.SelfHarnessAudit.ExpectedBehaviorChange != audit.ExpectedBehaviorChange ||
			entry.SelfHarnessAudit.RegressionRisk != audit.RegressionRisk {
			t.Fatalf("audit mismatch: got %+v want %+v", entry.SelfHarnessAudit, audit)
		}
	}
}

func TestSelfCorrectionCandidatesRecordAndReview(t *testing.T) {
	tracker := newTestTracker(t)
	rec, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:          "skill",
		SkillName:      "email-analysis",
		SessionKey:     "client:main",
		Title:          "Defer noisy mail rewrite",
		Candidate:      "tighten calendar extraction rules",
		Evidence:       "user corrected noisy schedule proposal",
		TargetFiles:    []string{"skills/productivity/email-analysis/SKILL.md", "skills/productivity/email-analysis/SKILL.md"},
		ProposedChange: "Add a conservative proposal-only rule",
		Risk:           "Could hide valid schedule candidates",
		Source:         "self-correction",
	})
	if err != nil {
		t.Fatalf("RecordSelfCorrectionCandidate: %v", err)
	}
	if rec.ID == "" || rec.Status != SelfCorrectionStatusProposed {
		t.Fatalf("candidate missing id/default status: %+v", rec)
	}
	if len(rec.TargetFiles) != 1 {
		t.Fatalf("target files should be cleaned/deduped, got %+v", rec.TargetFiles)
	}

	got, err := tracker.RecentSelfCorrectionCandidates("email-analysis", SelfCorrectionStatusProposed, 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	if len(got) != 1 || got[0].ID != rec.ID {
		t.Fatalf("expected pending candidate, got %+v", got)
	}

	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID:         rec.ID,
		Status:     SelfCorrectionStatusAccepted,
		Reviewer:   "codex",
		ReviewNote: "will batch with validation case",
	}); err != nil {
		t.Fatalf("RecordSelfCorrectionReview: %v", err)
	}
	pending, err := tracker.RecentSelfCorrectionCandidates("email-analysis", SelfCorrectionStatusProposed, 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("accepted candidate must leave pending queue, got %+v", pending)
	}
	accepted, err := tracker.RecentSelfCorrectionCandidates("email-analysis", SelfCorrectionStatusAccepted, 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates accepted: %v", err)
	}
	if len(accepted) != 1 || accepted[0].Reviewer != "codex" || accepted[0].ReviewNote == "" {
		t.Fatalf("review fields not merged into candidate: %+v", accepted)
	}
}

func TestSelfCorrectionReviewRejectsUnknownID(t *testing.T) {
	tracker := newTestTracker(t)

	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID:       "missing-candidate",
		Status:   SelfCorrectionStatusAccepted,
		Reviewer: "codex",
	}); err == nil || !strings.Contains(err.Error(), "self-correction candidate not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}

	got, err := tracker.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("orphan review should not create a visible candidate, got %+v", got)
	}
}

func TestSkillsNeedingEvolution_SkipsUntilNewRealFailureAfterAttempt(t *testing.T) {
	tracker := newTestTracker(t)
	beforeAttempt := time.Now().Add(-2 * time.Second).UnixMilli()
	for i := 0; i < 3; i++ {
		if err := tracker.RecordUsage(UsageRecord{
			SkillName: "topsolar-db",
			Success:   false,
			ErrorMsg:  "old real failure",
			UsedAt:    beforeAttempt + int64(i),
		}); err != nil {
			t.Fatalf("RecordUsage old failure: %v", err)
		}
	}
	if err := tracker.LogEvolveRejected("topsolar-db", "judge rejected candidate"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}

	candidates, err := tracker.SkillsNeedingEvolution(3, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.SkillName == "topsolar-db" {
			t.Fatalf("expected old failures to be suppressed after an evolve attempt, got %+v", candidates)
		}
	}

	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "topsolar-db",
		Success:   false,
		ErrorMsg:  "fresh real failure",
		UsedAt:    time.Now().Add(2 * time.Second).UnixMilli(),
	}); err != nil {
		t.Fatalf("RecordUsage fresh failure: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := tracker.RecordUsage(UsageRecord{
			SkillName: "topsolar-db",
			Success:   true,
			UsedAt:    time.Now().Add(time.Duration(3+i) * time.Second).UnixMilli(),
		}); err != nil {
			t.Fatalf("RecordUsage fresh success: %v", err)
		}
	}
	candidates, err = tracker.SkillsNeedingEvolution(3, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution after fresh evidence: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.SkillName == "topsolar-db" {
			return
		}
	}
	t.Fatalf("expected fresh post-attempt evidence to make skill eligible again, got %+v", candidates)
}

func TestSkillsNeedingEvolution_IgnoresExpiredFailureEvidence(t *testing.T) {
	t.Setenv("DENEB_SKILL_EVOLVE_EVIDENCE_DAYS", "7")
	tracker := newTestTracker(t)
	old := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	for i := 0; i < 3; i++ {
		if err := tracker.RecordUsage(UsageRecord{
			SkillName: "stale-skill",
			Success:   false,
			ErrorMsg:  "old real failure",
			UsedAt:    old + int64(i),
		}); err != nil {
			t.Fatalf("RecordUsage old failure: %v", err)
		}
	}

	stats, err := tracker.EvolutionEvidenceStats("stale-skill")
	if err != nil {
		t.Fatalf("EvolutionEvidenceStats: %v", err)
	}
	if stats.TotalUses != 0 {
		t.Fatalf("expired failures must not enter evolution evidence, got %+v", stats)
	}
	candidates, err := tracker.SkillsNeedingEvolution(2, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.SkillName == "stale-skill" {
			t.Fatalf("expired failures must not trigger evolution, got %+v", candidates)
		}
	}

	fresh := time.Now().UnixMilli()
	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "stale-skill",
		Success:   false,
		ErrorMsg:  "fresh real failure",
		UsedAt:    fresh,
	}); err != nil {
		t.Fatalf("RecordUsage fresh failure: %v", err)
	}
	candidates, err = tracker.SkillsNeedingEvolution(2, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution after one fresh failure: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.SkillName == "stale-skill" {
			t.Fatalf("one fresh use plus expired failures must not meet min evidence, got %+v", candidates)
		}
	}

	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "stale-skill",
		Success:   true,
		UsedAt:    fresh + 1,
	}); err != nil {
		t.Fatalf("RecordUsage fresh success: %v", err)
	}
	candidates, err = tracker.SkillsNeedingEvolution(2, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution after bounded evidence: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.SkillName == "stale-skill" {
			if candidate.TotalUses != 2 || candidate.FailureCount != 1 || candidate.SuccessCount != 1 {
				t.Fatalf("candidate must use bounded evidence only, got %+v", candidate)
			}
			return
		}
	}
	t.Fatalf("expected fresh bounded evidence to trigger candidate, got %+v", candidates)
}

func TestSkillsNeedingEvolution_SkipsThrashingTopSkillDuringCooldown(t *testing.T) {
	tracker := newTestTracker(t)
	for i := 0; i < evolutionThrashMinEvolves; i++ {
		if err := tracker.LogEvolve("deploy-helper", "1.0.1", "tighten deploy workflow"); err != nil {
			t.Fatalf("LogEvolve(%d): %v", i, err)
		}
	}
	freshFailureAt := time.Now().Add(2 * time.Second).UnixMilli()
	for _, skillName := range []string{"deploy-helper", "calendar-helper"} {
		for i := 0; i < 3; i++ {
			if err := tracker.RecordUsage(UsageRecord{
				SkillName:  skillName,
				SessionKey: "client:main",
				Success:    false,
				ErrorMsg:   "fresh real failure",
				UsedAt:     freshFailureAt + int64(i),
			}); err != nil {
				t.Fatalf("RecordUsage(%s): %v", skillName, err)
			}
		}
	}

	candidates, err := tracker.SkillsNeedingEvolution(3, 0.7)
	if err != nil {
		t.Fatalf("SkillsNeedingEvolution: %v", err)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		seen[candidate.SkillName] = true
	}
	if seen["deploy-helper"] {
		t.Fatalf("expected thrashing top skill to cool down, got candidates %+v", candidates)
	}
	if !seen["calendar-helper"] {
		t.Fatalf("expected non-thrashing failed skill to remain eligible, got candidates %+v", candidates)
	}
	health := tracker.EvolutionHealth()
	if !health.Thrash || health.TopEvolvedSkill != "deploy-helper" || health.ThrashCooldownUntil <= time.Now().UnixMilli() {
		t.Fatalf("expected deploy-helper thrash cooldown in health summary, got %+v", health)
	}
}

func TestTrackerRecentLifecycleLog_Limit(t *testing.T) {
	tracker := newTestTracker(t)
	for _, name := range []string{"one", "two", "three"} {
		if err := tracker.LogGenesis(name, "session", "", "coding", ""); err != nil {
			t.Fatalf("LogGenesis(%s): %v", name, err)
		}
	}

	entries, err := tracker.RecentLifecycleLog(2)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].SkillName != "three" || entries[1].SkillName != "two" {
		t.Fatalf("expected newest two entries, got %+v", entries)
	}
}

func TestTrackerRejectedSkillEdits_NewestFirstAndFiltered(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName:     "deploy-helper",
		Reason:        "missing verification gate",
		CandidateBody: strings.Repeat("x", 2500),
		Source:        "self-test",
	}); err != nil {
		t.Fatalf("RecordRejectedSkillEdit(first): %v", err)
	}
	if err := tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName:     "other-skill",
		Reason:        "irrelevant",
		CandidateBody: "other",
	}); err != nil {
		t.Fatalf("RecordRejectedSkillEdit(other): %v", err)
	}
	if err := tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName:     "deploy-helper",
		Reason:        "invented command",
		CandidateBody: "newer",
		Source:        "teacher",
	}); err != nil {
		t.Fatalf("RecordRejectedSkillEdit(second): %v", err)
	}

	entries, err := tracker.RecentRejectedSkillEdits("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentRejectedSkillEdits: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 deploy-helper rejected edits, got %+v", entries)
	}
	if entries[0].Reason != "invented command" || entries[1].Reason != "missing verification gate" {
		t.Fatalf("expected newest first, got %+v", entries)
	}
	if len([]rune(entries[1].CandidateBody)) != 2000 {
		t.Fatalf("expected candidate body truncated to 2000 runes, got %d", len([]rune(entries[1].CandidateBody)))
	}
}

func TestTrackerSkillOpportunities_NewestFirstAndFiltered(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Candidate:  "add merge verification proof",
		Route:      "evolve",
		SkillName:  "pr-merge-flow",
		SessionKey: "client:main",
		Evidence:   "user asked if it really merged",
	}); err != nil {
		t.Fatalf("RecordSkillOpportunity(first): %v", err)
	}
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Candidate: "other",
		Route:     "genesis",
		SkillName: "other-skill",
	}); err != nil {
		t.Fatalf("RecordSkillOpportunity(other): %v", err)
	}
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Candidate: "repeat origin/main proof",
		Route:     "evolve",
		SkillName: "pr-merge-flow",
		Reason:    "same near-miss repeated",
	}); err != nil {
		t.Fatalf("RecordSkillOpportunity(second): %v", err)
	}

	records, err := tracker.RecentSkillOpportunities("pr-merge-flow", 5)
	if err != nil {
		t.Fatalf("RecentSkillOpportunities: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 filtered opportunities, got %+v", records)
	}
	if records[0].Candidate != "repeat origin/main proof" || records[1].Candidate != "add merge verification proof" {
		t.Fatalf("opportunities not newest-first: %+v", records)
	}
	if records[0].Type != "skill_opportunity" || records[0].Source != "proposal" {
		t.Fatalf("expected default type/source, got %+v", records[0])
	}
}

func TestTrackerRejectedSkillEditsFallsBackToLifecycleLog(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogEvolveRejected("deploy-helper", "judge rejected candidate"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}

	entries, err := tracker.RecentRejectedSkillEdits("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentRejectedSkillEdits: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one lifecycle fallback rejected edit, got %+v", entries)
	}
	if entries[0].Reason != "judge rejected candidate" || entries[0].Source != "lifecycle-fallback" {
		t.Fatalf("unexpected lifecycle fallback rejected edit: %+v", entries[0])
	}
}

func TestTrackerSkillValidationCases_NewestFirstAndFiltered(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "deploy-helper",
		ID:        "empty",
	}); err == nil {
		t.Fatal("expected validation case without assertions to be rejected")
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:          "deploy-helper",
		ID:                 "verify-health",
		RequiredSubstrings: []string{"verify health"},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(first): %v", err)
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:          "other-skill",
		ID:                 "other",
		RequiredSubstrings: []string{"other"},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(other): %v", err)
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:           "deploy-helper",
		ID:                  "safe-shell",
		ForbiddenSubstrings: []string{"eval"},
		Replay: SkillReplayCaseRecord{
			Input:                "Deploy and verify the service.",
			RequiredActions:      []string{"verify health"},
			RequiredTools:        []string{"curl"},
			RequiredObservations: []string{"200 OK"},
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec", InputIncludes: []string{"curl /health"}, FixtureOutput: "200 OK"},
			},
			RequireOrder: true,
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(second): %v", err)
	}

	cases, err := tracker.RecentSkillValidationCases("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 deploy-helper validation cases, got %+v", cases)
	}
	if cases[0].ID != "safe-shell" || cases[1].ID != "verify-health" {
		t.Fatalf("expected newest first, got %+v", cases)
	}
	if cases[0].Replay.Input != "Deploy and verify the service." ||
		len(cases[0].Replay.RequiredActions) != 1 ||
		len(cases[0].Replay.RequiredTools) != 1 ||
		len(cases[0].Replay.RequiredObservations) != 1 ||
		len(cases[0].Replay.ExpectedToolCalls) != 1 ||
		cases[0].Replay.ExpectedToolCalls[0].Name != "exec" ||
		len(cases[0].Replay.ExpectedToolCalls[0].InputIncludes) != 1 ||
		cases[0].Replay.ExpectedToolCalls[0].FixtureOutput != "200 OK" ||
		!cases[0].Replay.RequireOrder {
		t.Fatalf("expected replay fields to round-trip, got %+v", cases[0].Replay)
	}
}

func TestTrackerSkillValidationCasesDedupesLatestByIDAndPayload(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:          "deploy-helper",
		ID:                 "verify-health",
		Description:        "older copy",
		RequiredSubstrings: []string{"verify health"},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(id older): %v", err)
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:          "deploy-helper",
		ID:                 "verify-health",
		Description:        "newer copy",
		RequiredSubstrings: []string{"verify health"},
		Source:             "review-session",
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(id newer): %v", err)
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:        "deploy-helper",
		Description:      "manual duplicate older",
		RequiredHeadings: []string{"Verification"},
		Replay: SkillReplayCaseRecord{
			RequiredActions: []string{"curl /health"},
			RequiredTools:   []string{"exec"},
		},
		Source: "operator",
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(payload older): %v", err)
	}
	if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:        "deploy-helper",
		Description:      "manual duplicate newer",
		RequiredHeadings: []string{"Verification"},
		Replay: SkillReplayCaseRecord{
			RequiredActions: []string{"curl /health"},
			RequiredTools:   []string{"exec"},
		},
		Source: "review-session",
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase(payload newer): %v", err)
	}

	cases, err := tracker.RecentSkillValidationCases("deploy-helper", 10)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected duplicate records to collapse to 2 unique cases, got %+v", cases)
	}
	if cases[0].Description != "manual duplicate newer" || cases[1].Description != "newer copy" {
		t.Fatalf("expected newest unique cases, got %+v", cases)
	}
}

func TestTrackerValidationCaseSummaryRejectsWeakAutomaticCases(t *testing.T) {
	tracker := newTestTracker(t)
	err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "deploy-helper",
		ID:        "weak-auto",
		Replay: SkillReplayCaseRecord{
			RequiredTools: []string{"exec"},
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec"},
			},
		},
		Source: "review-session",
	})
	if !errors.Is(err, ErrWeakAutomaticValidationCase) {
		t.Fatalf("expected weak automatic validation case rejection, got %v", err)
	}

	for _, desc := range []string{"older valid trace", "newer valid trace"} {
		if err := tracker.RecordSkillValidationCase(SkillValidationCaseRecord{
			SkillName:   "deploy-helper",
			ID:          "valid-auto",
			Description: desc,
			Replay: SkillReplayCaseRecord{
				ExpectedToolCalls: []SkillReplayToolCallRecord{
					{Name: "exec", InputIncludes: []string{"curl /health"}},
				},
			},
			Source: "session-backfill",
		}); err != nil {
			t.Fatalf("RecordSkillValidationCase(%s): %v", desc, err)
		}
	}

	summary, err := tracker.ValidationCaseSummary("deploy-helper")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 2 ||
		summary.UniqueRecords != 1 ||
		summary.DuplicateRecords != 1 ||
		summary.AutomaticRecords != 2 ||
		summary.UniqueAutomaticRecords != 1 ||
		summary.WeakAutomaticRecords != 0 ||
		summary.UniqueWeakAutomaticCases != 0 {
		t.Fatalf("unexpected validation summary: %+v", summary)
	}
	cases, err := tracker.RecentSkillValidationCases("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 || cases[0].Description != "newer valid trace" {
		t.Fatalf("expected latest de-duped validation case, got %+v", cases)
	}
}

func TestTrackerValidationCaseSummaryCountsFrontierTiers(t *testing.T) {
	tracker := newTestTracker(t)
	for _, rec := range []SkillValidationCaseRecord{
		{
			SkillName:          "deploy-helper",
			ID:                 "easy",
			FrontierTier:       "easy_anchor",
			RequiredSubstrings: []string{"origin/main"},
			Source:             "operator",
		},
		{
			SkillName:          "deploy-helper",
			ID:                 "mixed",
			FrontierTier:       "mixed-frontier",
			RequiredSubstrings: []string{"verify listener"},
			Source:             "operator",
		},
		{
			SkillName:          "deploy-helper",
			ID:                 "hard",
			FrontierTier:       "hard",
			RequiredSubstrings: []string{"unknown future signal"},
			Source:             "operator",
		},
	} {
		if err := tracker.RecordSkillValidationCase(rec); err != nil {
			t.Fatalf("RecordSkillValidationCase(%s): %v", rec.ID, err)
		}
	}

	summary, err := tracker.ValidationCaseSummary("deploy-helper")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.UniqueEasyAnchorCases != 1 ||
		summary.UniqueMixedFrontierCases != 1 ||
		summary.UniqueHardFrontierCases != 1 {
		t.Fatalf("unexpected frontier tier summary: %+v", summary)
	}
	cases, err := tracker.RecentSkillValidationCases("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	tiers := map[string]bool{}
	for _, tc := range cases {
		tiers[tc.FrontierTier] = true
	}
	if !tiers["easy"] || !tiers["mixed"] || !tiers["hard"] {
		t.Fatalf("frontier tiers were not normalized in stored cases: %+v", cases)
	}
}

func TestTrackerOptimizerMemoryRecordsAcceptedRejectedAndRollback(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogEvolve("deploy-helper", "1.0.1", "tighten verification steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.LogEvolveRejected("deploy-helper", "invented command"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}
	if err := tracker.LogEvolveRolledBack("deploy-helper"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}

	memory, err := tracker.OptimizerMemory("deploy-helper")
	if err != nil {
		t.Fatalf("OptimizerMemory: %v", err)
	}
	if memory.AcceptedCount != 1 || memory.RejectedCount != 1 || memory.RolledBackCount != 1 {
		t.Fatalf("unexpected optimizer memory counts: %+v", memory)
	}
	if len(memory.StableDirections) != 1 || memory.StableDirections[0] != "tighten verification steps" {
		t.Fatalf("unexpected stable directions: %+v", memory.StableDirections)
	}
	if len(memory.AvoidDirections) != 2 || memory.AvoidDirections[0] != "post-evolve rollback fired" || memory.AvoidDirections[1] != "invented command" {
		t.Fatalf("unexpected avoid directions: %+v", memory.AvoidDirections)
	}
}

func TestTrackerOptimizerMemoryFallsBackToLifecycleLogWhenSidecarMissing(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogEvolve("deploy-helper", "1.0.1", "tighten verification steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.LogEvolveRejected("deploy-helper", "invented command"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}
	if err := tracker.LogEvolveRolledBack("deploy-helper"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}
	if err := os.Remove(tracker.optimizerMemoryPath); err != nil {
		t.Fatalf("remove optimizer memory sidecar: %v", err)
	}

	memory, err := tracker.OptimizerMemory("deploy-helper")
	if err != nil {
		t.Fatalf("OptimizerMemory: %v", err)
	}
	if memory.AcceptedCount != 1 || memory.RejectedCount != 1 || memory.RolledBackCount != 1 {
		t.Fatalf("unexpected lifecycle fallback optimizer memory counts: %+v", memory)
	}
	if len(memory.StableDirections) != 1 || memory.StableDirections[0] != "tighten verification steps" {
		t.Fatalf("unexpected fallback stable directions: %+v", memory.StableDirections)
	}
	if len(memory.AvoidDirections) != 2 || memory.AvoidDirections[0] != "post-evolve rollback fired" || memory.AvoidDirections[1] != "invented command" {
		t.Fatalf("unexpected fallback avoid directions: %+v", memory.AvoidDirections)
	}
}

func TestTrackerOptimizerMemoryBackfillsLifecycleBeforeFirstSidecarWrite(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogEvolve("deploy-helper", "1.0.1", "old accepted direction"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.LogEvolveRejected("deploy-helper", "old rejected direction"); err != nil {
		t.Fatalf("LogEvolveRejected(old): %v", err)
	}
	if err := os.Remove(tracker.optimizerMemoryPath); err != nil {
		t.Fatalf("remove optimizer memory sidecar: %v", err)
	}
	if err := tracker.LogEvolveRejected("deploy-helper", "new rejected direction"); err != nil {
		t.Fatalf("LogEvolveRejected(new): %v", err)
	}

	memory, err := tracker.OptimizerMemory("deploy-helper")
	if err != nil {
		t.Fatalf("OptimizerMemory: %v", err)
	}
	if memory.AcceptedCount != 1 || memory.RejectedCount != 2 || memory.RolledBackCount != 0 {
		t.Fatalf("unexpected backfilled optimizer memory counts: %+v", memory)
	}
	if len(memory.StableDirections) != 1 || memory.StableDirections[0] != "old accepted direction" {
		t.Fatalf("unexpected backfilled stable directions: %+v", memory.StableDirections)
	}
	if len(memory.AvoidDirections) != 2 || memory.AvoidDirections[0] != "new rejected direction" || memory.AvoidDirections[1] != "old rejected direction" {
		t.Fatalf("unexpected backfilled avoid directions: %+v", memory.AvoidDirections)
	}
}

func TestTrackerCuratorMarksGeneratedSkillsAndTracksActivity(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "deploy-helper",
		Success:   true,
		UsedAt:    time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := tracker.MarkSkillPatched("deploy-helper"); err != nil {
		t.Fatalf("MarkSkillPatched: %v", err)
	}

	report, err := tracker.SkillCuratorReport("deploy-helper")
	if err != nil {
		t.Fatalf("SkillCuratorReport: %v", err)
	}
	if len(report) != 1 {
		t.Fatalf("expected one curator record, got %+v", report)
	}
	rec := report[0]
	if rec.CreatedBy != SkillCuratorCreatedByAgent || rec.State != SkillCuratorStateActive {
		t.Fatalf("unexpected curator identity/state: %+v", rec)
	}
	if rec.UseCount != 1 || rec.PatchCount != 1 {
		t.Fatalf("expected use and patch counts, got %+v", rec)
	}
}

func TestTrackerCuratorTransitionsOnlyAgentCreatedAndHonorsPin(t *testing.T) {
	tracker := newTestTracker(t)
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour).UnixMilli()

	if err := tracker.markSkillAgentCreatedLockedForTest("agent-skill", old); err != nil {
		t.Fatalf("mark agent-skill: %v", err)
	}
	if err := tracker.markSkillAgentCreatedLockedForTest("pinned-skill", old); err != nil {
		t.Fatalf("mark pinned-skill: %v", err)
	}
	if err := tracker.SetSkillPinned("pinned-skill", true); err != nil {
		t.Fatalf("SetSkillPinned: %v", err)
	}
	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "user-skill",
		Success:   true,
		UsedAt:    old,
	}); err != nil {
		t.Fatalf("RecordUsage user-skill: %v", err)
	}

	summary, err := tracker.ApplySkillCuratorTransitions(now, SkillCuratorConfig{
		IntervalHours:    1,
		MinIdleHours:     0,
		StaleAfterDays:   30,
		ArchiveAfterDays: 90,
	})
	if err != nil {
		t.Fatalf("ApplySkillCuratorTransitions: %v", err)
	}
	if summary.Archived != 1 || summary.Checked != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	report, err := tracker.SkillCuratorReport("")
	if err != nil {
		t.Fatalf("SkillCuratorReport: %v", err)
	}
	states := map[string]string{}
	for _, rec := range report {
		states[rec.SkillName] = rec.State
	}
	if states["agent-skill"] != SkillCuratorStateArchived {
		t.Fatalf("agent-skill should be archived, got %q", states["agent-skill"])
	}
	if states["pinned-skill"] != SkillCuratorStateActive {
		t.Fatalf("pinned-skill should remain active, got %q", states["pinned-skill"])
	}
	if _, ok := states["user-skill"]; ok {
		t.Fatalf("user-skill should not be curator-managed: %+v", report)
	}
}

// TestUsageStats_ExcludeConsultInfraFailures verifies that "tool skills errored"
// failures (the skill could not be loaded — a gateway path/catalog bug, not the
// skill's fault) are dropped from a skill's usage aggregate. Counting them
// pinned email-analysis below the evolver's success-rate threshold and triggered
// a phantom re-evolution every 6h that chased an error the skill cannot fix.
func TestUsageStats_ExcludeConsultInfraFailures(t *testing.T) {
	tr := newTestTracker(t)
	rec := func(ok bool, errMsg string) {
		if err := tr.RecordUsage(UsageRecord{SkillName: "email-analysis", Success: ok, ErrorMsg: errMsg}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	rec(true, "")                                  // real success
	rec(false, "turn failed: tool skills errored") // infra → dropped
	rec(false, "turn failed: tool skills errored") // infra → dropped
	rec(false, "wiki write failed")                // real failure → counts

	stats, err := tr.Stats("email-analysis")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalUses != 2 || stats.SuccessCount != 1 || stats.FailureCount != 1 {
		t.Fatalf("infra failures not excluded from aggregate: %+v", stats)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5 (infra failures excluded)", stats.SuccessRate)
	}
	for _, e := range stats.RecentErrors {
		if isConsultInfraError(e) {
			t.Fatalf("recentErrors must not surface an infra error to the judge: %q", e)
		}
	}
}

func TestUsageStats_ExcludeUnactionableLegacyFailures(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordUsage(UsageRecord{SkillName: "topsolar-db", Success: true}); err != nil {
		t.Fatalf("RecordUsage success: %v", err)
	}
	for i := 0; i < 20; i++ {
		if err := tr.RecordUsage(UsageRecord{SkillName: "topsolar-db", Success: false}); err != nil {
			t.Fatalf("RecordUsage empty legacy failure: %v", err)
		}
	}
	if err := tr.RecordUsage(UsageRecord{
		SkillName: "topsolar-db",
		Success:   false,
		ErrorMsg:  "sqlite database missing",
	}); err != nil {
		t.Fatalf("RecordUsage actionable failure: %v", err)
	}

	stats, err := tr.Stats("topsolar-db")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalUses != 2 || stats.SuccessCount != 1 || stats.FailureCount != 1 {
		t.Fatalf("empty legacy failures should be ignored but actionable failure kept: %+v", stats)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5", stats.SuccessRate)
	}
	if len(stats.RecentErrors) != 1 || stats.RecentErrors[0] != "sqlite database missing" {
		t.Fatalf("recent errors should keep only actionable failure, got %+v", stats.RecentErrors)
	}
}

func TestUsageQualitySummaryReportsIgnoredRecords(t *testing.T) {
	tr := newTestTracker(t)
	records := []UsageRecord{
		{SkillName: "topsolar-db", Success: true},
		{SkillName: "topsolar-db", Success: false},
		{SkillName: "topsolar-db", Success: false},
		{SkillName: "topsolar-db", Success: true, Source: UsageSourceReviewVerdict, SessionKey: reviewSessionPrefix + "1"},
		{SkillName: "email-analysis", Success: false, ErrorMsg: "turn failed: tool skills errored"},
	}
	for _, record := range records {
		if err := tr.RecordUsage(record); err != nil {
			t.Fatalf("RecordUsage(%+v): %v", record, err)
		}
	}

	global, err := tr.UsageQualitySummary("")
	if err != nil {
		t.Fatalf("UsageQualitySummary(global): %v", err)
	}
	if global.TotalRecords != 5 || global.CountedRecords != 1 || global.IgnoredRecords != 4 {
		t.Fatalf("unexpected global quality counts: %+v", global)
	}
	if global.IgnoredReviewRecords != 1 || global.IgnoredConsultInfraFailures != 1 || global.IgnoredUnactionableLegacyFailures != 2 {
		t.Fatalf("unexpected ignored breakdown: %+v", global)
	}
	if global.TopIgnoredUnactionableLegacyFailureSkill != "topsolar-db" || global.TopIgnoredUnactionableLegacyFailureSkillCount != 2 {
		t.Fatalf("unexpected top ignored legacy failure skill: %+v", global)
	}

	filtered, err := tr.UsageQualitySummary("topsolar-db")
	if err != nil {
		t.Fatalf("UsageQualitySummary(topsolar-db): %v", err)
	}
	if filtered.SkillName != "topsolar-db" || filtered.TotalRecords != 4 || filtered.CountedRecords != 1 || filtered.IgnoredUnactionableLegacyFailures != 2 {
		t.Fatalf("unexpected filtered quality counts: %+v", filtered)
	}
}

// TestPostEvolveRollback_IgnoresConsultInfraFailures verifies an infra failure
// (skill couldn't be loaded) never counts toward a post-evolve rollback, so a
// good evolution is not reverted by a gateway-side consult error.
func TestPostEvolveRollback_IgnoresConsultInfraFailures(t *testing.T) {
	tr := newTestTracker(t)
	fired := make(chan string, 1)
	tr.SetRollback(func(s string) { fired <- s }, 3)
	if err := tr.LogEvolve("email-analysis", "1.1.3", "tighten steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := tr.RecordUsage(UsageRecord{
			SkillName: "email-analysis",
			Success:   false,
			ErrorMsg:  "turn failed: tool skills errored",
		}); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	select {
	case got := <-fired:
		t.Fatalf("rollback fired (%q) on infra failures that are not the skill's fault", got)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestUsageStats_ExcludeReviewSources verifies the evolver's success-rate sees
// only genuine skill executions: review-fork verdicts (explicit Source) and
// consult turns (session prefix) and consult-infra failures are all excluded.
func TestUsageStats_ExcludeReviewSources(t *testing.T) {
	tr := newTestTracker(t)
	rec := func(r UsageRecord) {
		if err := tr.RecordUsage(r); err != nil {
			t.Fatalf("RecordUsage: %v", err)
		}
	}
	rec(UsageRecord{SkillName: "s", SessionKey: "client:main", Success: true})                            // real success
	rec(UsageRecord{SkillName: "s", SessionKey: "cron:x", Success: false, ErrorMsg: "wiki write failed"}) // real failure
	// review verdict (explicit Source) → excluded
	rec(UsageRecord{SkillName: "s", SessionKey: "system:skill-review:client:main", Success: false, Source: UsageSourceReviewVerdict})
	// review fork consult (session prefix, legacy untagged) → excluded
	rec(UsageRecord{SkillName: "s", SessionKey: "system:skill-review:cron:y", Success: false, ErrorMsg: "turn failed: tool skills errored"})
	// real session but consult-infra failure → excluded
	rec(UsageRecord{SkillName: "s", SessionKey: "cron:z", Success: false, ErrorMsg: "turn failed: tool skills errored"})

	stats, err := tr.Stats("s")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalUses != 2 || stats.SuccessCount != 1 || stats.FailureCount != 1 {
		t.Fatalf("only real use should count: %+v", stats)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5", stats.SuccessRate)
	}
}

// TestEvolutionHealth_DetectsThrash verifies the /health summary flags a thrash
// when one skill dominates recent evolutions (the email-analysis pattern).
func TestEvolutionHealth_DetectsThrash(t *testing.T) {
	tr := newTestTracker(t)
	for i := 0; i < 6; i++ {
		if err := tr.LogEvolve("email-analysis", "1.1.0", "x"); err != nil {
			t.Fatalf("LogEvolve: %v", err)
		}
	}
	if err := tr.LogEvolve("other-skill", "1.0.1", "y"); err != nil {
		t.Fatalf("LogEvolve other: %v", err)
	}
	eh := tr.EvolutionHealth()
	if eh.Evolves7d != 7 || eh.DistinctSkillsEvolved7d != 2 {
		t.Fatalf("unexpected counts: %+v", eh)
	}
	if eh.TopEvolvedSkill != "email-analysis" || eh.TopEvolvedCount != 6 {
		t.Fatalf("unexpected top skill: %+v", eh)
	}
	if !eh.Thrash {
		t.Fatalf("expected thrash (one skill = 6/7 evolves): %+v", eh)
	}
}

// TestEvolutionHealth_BalancedIsNotThrash verifies evenly spread evolutions do
// not trip the thrash flag.
func TestEvolutionHealth_BalancedIsNotThrash(t *testing.T) {
	tr := newTestTracker(t)
	for _, name := range []string{"a", "a", "b", "b", "c", "c"} {
		if err := tr.LogEvolve(name, "1.0.1", ""); err != nil {
			t.Fatalf("LogEvolve: %v", err)
		}
	}
	eh := tr.EvolutionHealth()
	if eh.Evolves7d != 6 || eh.DistinctSkillsEvolved7d != 3 {
		t.Fatalf("unexpected counts: %+v", eh)
	}
	if eh.Thrash {
		t.Fatalf("balanced evolves must not flag thrash: %+v", eh)
	}
}

// TestEvolutionHealth_SingleSkillDominanceIsThrash verifies the low-volume but
// total-dominance case (one skill is the ONLY thing evolving) trips the flag —
// the real email-analysis state, where nothing else ever gets attention.
func TestEvolutionHealth_SingleSkillDominanceIsThrash(t *testing.T) {
	tr := newTestTracker(t)
	for i := 0; i < 3; i++ {
		if err := tr.LogEvolve("email-analysis", "1.1.0", ""); err != nil {
			t.Fatalf("LogEvolve: %v", err)
		}
	}
	eh := tr.EvolutionHealth()
	if eh.DistinctSkillsEvolved7d != 1 || eh.Evolves7d != 3 {
		t.Fatalf("unexpected counts: %+v", eh)
	}
	if !eh.Thrash {
		t.Fatalf("one skill = 100%% of evolves must flag thrash: %+v", eh)
	}
}

func TestEvolutionHealth_IncludesRejectedAndRolledBackSignals(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogEvolveRejected("email-analysis", "textual edit budget exceeded"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}
	if err := tr.LogEvolveRolledBack("deploy-helper"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}

	eh := tr.EvolutionHealth()
	if eh.EvolveRejected7d != 1 || eh.EvolveRolledBack7d != 1 {
		t.Fatalf("unexpected reject/rollback counts: %+v", eh)
	}
	if eh.LastRejectedSkill != "email-analysis" || eh.LastRejectedReason != "textual edit budget exceeded" {
		t.Fatalf("unexpected last rejected summary: %+v", eh)
	}
}

// TestCuratorUseCount_RealUseOnly verifies a review verdict does NOT bump the
// curator use-count/staleness (only genuine execution does), so a verdict-only
// skill correctly reads as unused and stays eligible for staleness pruning.
func TestCuratorUseCount_RealUseOnly(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogGenesis("gen-skill", "session", "client:main", "productivity", "x"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	if err := tr.RecordUsage(UsageRecord{SkillName: "gen-skill", SessionKey: "system:skill-review:client:main", Success: true, Source: UsageSourceReviewVerdict}); err != nil {
		t.Fatalf("RecordUsage verdict: %v", err)
	}
	report, err := tr.SkillCuratorReport("gen-skill")
	if err != nil || len(report) != 1 {
		t.Fatalf("report: %v %+v", err, report)
	}
	if report[0].UseCount != 0 {
		t.Fatalf("a review verdict must not bump curator use-count: %+v", report[0])
	}
	if err := tr.RecordUsage(UsageRecord{SkillName: "gen-skill", SessionKey: "client:main", Success: true}); err != nil {
		t.Fatalf("RecordUsage real: %v", err)
	}
	report, _ = tr.SkillCuratorReport("gen-skill")
	if report[0].UseCount != 1 {
		t.Fatalf("real use must bump curator use-count to 1: %+v", report[0])
	}
}

// TestAgentSkillValueSummary verifies the /health "dead weight" measurement:
// agent-created skills total + how many have zero real uses.
func TestAgentSkillValueSummary(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.LogGenesis("used-skill", "session", "client:main", "productivity", ""); err != nil {
		t.Fatalf("LogGenesis used: %v", err)
	}
	if err := tr.LogGenesis("dead-skill", "session", "client:main", "productivity", ""); err != nil {
		t.Fatalf("LogGenesis dead: %v", err)
	}
	if err := tr.RecordUsage(UsageRecord{SkillName: "used-skill", SessionKey: "client:main", Success: true}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	total, unused := tr.AgentSkillValueSummary()
	if total != 2 || unused != 1 {
		t.Fatalf("expected 2 agent skills / 1 unused, got total=%d unused=%d", total, unused)
	}
}

// TestReconcileCuratorAgainstCatalog verifies orphan curator entries (skills no
// longer in the catalog) are pruned while present ones are kept.
func TestReconcileCuratorAgainstCatalog(t *testing.T) {
	tr := newTestTracker(t)
	for _, n := range []string{"keep-skill", "orphan-skill"} {
		if err := tr.LogGenesis(n, "session", "client:main", "productivity", ""); err != nil {
			t.Fatalf("LogGenesis(%s): %v", n, err)
		}
	}
	pruned, err := tr.ReconcileCuratorAgainstCatalog(map[string]bool{"keep-skill": true})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "orphan-skill" {
		t.Fatalf("expected to prune [orphan-skill], got %v", pruned)
	}
	report, _ := tr.SkillCuratorReport("")
	names := map[string]bool{}
	for _, r := range report {
		names[r.SkillName] = true
	}
	if !names["keep-skill"] || names["orphan-skill"] {
		t.Fatalf("after reconcile, curator should keep only keep-skill: %v", names)
	}
}

// TestReconcileCuratorAgainstCatalog_SkipsWhenAllMissing verifies the safety
// guard: an empty/failed catalog (every agent skill "missing") must NOT wipe the
// lifecycle history — discovery probably failed, not a mass removal.
func TestReconcileCuratorAgainstCatalog_SkipsWhenAllMissing(t *testing.T) {
	tr := newTestTracker(t)
	for _, n := range []string{"a", "b"} {
		if err := tr.LogGenesis(n, "session", "client:main", "productivity", ""); err != nil {
			t.Fatalf("LogGenesis(%s): %v", n, err)
		}
	}
	pruned, err := tr.ReconcileCuratorAgainstCatalog(map[string]bool{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(pruned) != 0 {
		t.Fatalf("must skip prune when all agent skills missing, pruned %v", pruned)
	}
	if report, _ := tr.SkillCuratorReport(""); len(report) != 2 {
		t.Fatalf("lifecycle history must be preserved, got %d entries", len(report))
	}
}

func (t *Tracker) markSkillAgentCreatedLockedForTest(skillName string, createdAt int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.markSkillAgentCreatedLocked(skillName, createdAt)
}
