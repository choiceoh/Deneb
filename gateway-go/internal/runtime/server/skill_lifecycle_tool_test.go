package server

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

func newSkillLifecycleTestTracker(t *testing.T) *genesis.Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return tracker
}

func TestSkillLifecycleStatusFiltersBySkillAndStats(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.RecordUsage(genesis.UsageRecord{
		SkillName:  "deploy-helper",
		SessionKey: "telegram:1",
		Success:    false,
		ErrorMsg:   "missing token",
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := tracker.LogGenesis("other-skill", "session", "", "coding", "Other workflow"); err != nil {
		t.Fatalf("LogGenesis(other): %v", err)
	}
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis(deploy): %v", err)
	}
	if err := tracker.LogEvolutionProposal(genesis.EvolutionProposalRecord{
		Candidate: "repeatable deploy fix",
		Route:     "evolve",
		SkillName: "deploy-helper",
		Executed:  true,
	}); err != nil {
		t.Fatalf("LogEvolutionProposal: %v", err)
	}

	backend := &skillLifecycleBackend{tracker: tracker}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "deploy-helper",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	recent := got["recent"].([]genesis.LifecycleLogEntry)
	if len(recent) != 2 {
		t.Fatalf("expected 2 deploy-helper lifecycle entries, got %+v", recent)
	}
	stats := got["stats"].(*genesis.UsageStats)
	if stats.SkillName != "deploy-helper" || stats.TotalUses != 1 || stats.SuccessRate != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	curator := got["curator"].([]genesis.SkillCuratorRecord)
	if len(curator) != 1 || curator[0].SkillName != "deploy-helper" {
		t.Fatalf("unexpected curator report: %+v", curator)
	}
}

func TestSkillLifecycleLogProposalStoresActualExecutionAndTruncates(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	backend := &skillLifecycleBackend{tracker: tracker}

	backend.logProposal(chattools.SkillEvolutionProposalRequest{
		Candidate: "manual create route",
		Route:     "create",
		Execute:   true,
	}, "create", map[string]any{
		"ok":        true,
		"executed":  false,
		"largeText": strings.Repeat("x", skillLifecycleMaxProposalResultBytes+100),
	})

	entries, err := tracker.RecentLifecycleLog(1)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one proposal entry, got %d", len(entries))
	}
	if entries[0].Executed {
		t.Fatalf("expected actual execution=false even when execute was requested: %+v", entries[0])
	}
	if !strings.HasSuffix(entries[0].Result, "...[truncated]") {
		t.Fatalf("expected truncated result, got length %d", len(entries[0].Result))
	}
}

func TestSkillLifecycleCuratorActionsPinArchiveRestore(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.LogGenesis("generated-helper", "session", "telegram:1", "coding", "Generated helper"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	backend := &skillLifecycleBackend{tracker: tracker}

	if _, err := backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "pin",
		SkillName: "generated-helper",
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	report, err := tracker.SkillCuratorReport("generated-helper")
	if err != nil {
		t.Fatalf("SkillCuratorReport: %v", err)
	}
	if len(report) != 1 || !report[0].Pinned {
		t.Fatalf("expected pinned curator record, got %+v", report)
	}

	gotAny, err := backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "archive",
		SkillName: "generated-helper",
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	got := gotAny.(map[string]any)
	rec := got["curator"].(genesis.SkillCuratorRecord)
	if rec.State != genesis.SkillCuratorStateArchived || rec.ArchivedAt == 0 {
		t.Fatalf("expected archived record, got %+v", rec)
	}

	gotAny, err = backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "restore",
		SkillName: "generated-helper",
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	got = gotAny.(map[string]any)
	rec = got["curator"].(genesis.SkillCuratorRecord)
	if rec.State != genesis.SkillCuratorStateActive || rec.ArchivedAt != 0 {
		t.Fatalf("expected restored active record, got %+v", rec)
	}
}

// TestProposeSkillEvolution_NoOpWithoutCandidate verifies that a no-op proposal
// succeeds without a candidate. A no-op records "no skill-worthy pattern,
// nothing to do", so a reusable candidate is optional by definition.
// Regression for the reviewer agent's repeated "candidate is required for
// propose" failures, which forced every no-op review to error out.
func TestProposeSkillEvolution_NoOpWithoutCandidate(t *testing.T) {
	// nil tracker/genesis: logProposal no-ops and no route executes.
	b := &skillLifecycleBackend{}
	res, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route:      "no-op",
		Reason:     "existing skill already covers this",
		SessionKey: "test:session",
	})
	if err != nil {
		t.Fatalf("no-op without candidate should succeed, got: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	if m["route"] != "no-op" {
		t.Errorf("expected route=no-op, got %v", m["route"])
	}
}

// TestProposeSkillEvolution_ExecutableRouteRequiresCandidate ensures executable
// routes (genesis/create/evolve) still require a candidate — the no-op
// exemption must not weaken validation for routes that actually do work.
func TestProposeSkillEvolution_ExecutableRouteRequiresCandidate(t *testing.T) {
	b := &skillLifecycleBackend{}
	_, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route:      "evolve",
		SessionKey: "test:session",
	})
	if err == nil {
		t.Fatal("expected error: candidate required for executable route, got nil")
	}
}

// TestProposeSkillEvolution_VerdictExcludedFromSuccessRate verifies a review
// verdict is recorded (it still drives the curator's staleness/lastUsed signal)
// but is NOT counted toward the evolver's success-rate stats. A judgment is not
// a real execution; conflating them pinned email-analysis as a phantom
// underperformer that re-evolved six times in two days (PR #2328). The
// success-rate now reflects real use only, so a pair of verdicts leaves it empty.
func TestProposeSkillEvolution_VerdictExcludedFromSuccessRate(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	b := &skillLifecycleBackend{tracker: tracker}

	if _, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route: "no-op", SkillName: "email-analysis", Reason: "skill already covers it", SessionKey: "s1",
	}); err != nil {
		t.Fatalf("no-op: %v", err)
	}
	if _, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route: "evolve", SkillName: "email-analysis", Candidate: "add category param", Reason: "category missing", SessionKey: "s2",
	}); err != nil {
		t.Fatalf("evolve: %v", err)
	}

	stats, err := tracker.Stats("email-analysis")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalUses != 0 {
		t.Fatalf("review verdicts must not feed the success rate, got total=%d", stats.TotalUses)
	}
}
