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
		stats:        make(map[string]*usageAgg),
		recentErrors: make(map[string][]string),
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

func (t *Tracker) markSkillAgentCreatedLockedForTest(skillName string, createdAt int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.markSkillAgentCreatedLocked(skillName, createdAt)
}
