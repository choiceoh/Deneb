package genesis

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	return &Tracker{
		logger:       slog.Default(),
		usagePath:    filepath.Join(dir, "skill_usage.jsonl"),
		logPath:      filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:  filepath.Join(dir, "skill_curator_state.json"),
		livenessPath: filepath.Join(dir, "skill_liveness.json"),
		stats:        make(map[string]*usageAgg),
		recentErrors: make(map[string][]string),
		postEvolve:   make(map[string]*evolveWatch),
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
