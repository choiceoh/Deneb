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
