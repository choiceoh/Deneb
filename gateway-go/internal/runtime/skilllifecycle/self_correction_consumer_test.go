package skilllifecycle

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// The review surface must say who will act on each candidate. Without it a
// reviewer cannot tell "queued for coding-dispatch" from "nothing will ever
// claim this" and defaults to accepted — how 22 candidates went silent
// (ledger audit 2026-08-01). Separate tests so one failure does not hide another.

func consumerFor(t *testing.T, rec genesis.SelfCorrectionCandidateRecord) string {
	t.Helper()
	tracker := newSkillLifecycleTestTracker(t)
	if _, err := tracker.RecordSelfCorrectionCandidate(rec); err != nil {
		t.Fatalf("RecordSelfCorrectionCandidate: %v", err)
	}
	backend := &skillLifecycleBackend{tracker: tracker}
	got, errStr := backend.recentSelfCorrectionCandidates(rec.SkillName, 10)
	if errStr != "" {
		t.Fatalf("recentSelfCorrectionCandidates: %s", errStr)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 open candidate, got %d", len(got))
	}
	return got[0].Consumer
}

func TestConsumerIsCodingDispatchForAllowlistedCodeSource(t *testing.T) {
	got := consumerFor(t, genesis.SelfCorrectionCandidateRecord{
		Scope: "code", SkillName: "gateway-runtime", Source: "runtime-error:abc123",
		Title: "recurring runtime error",
	})
	if got != "coding-dispatch" {
		t.Errorf("allowlisted code source should be claimable, got %q", got)
	}
}

func TestConsumerIsNoneForCodeSourceOffTheAllowlist(t *testing.T) {
	// sop-mining is not on the dispatch allowlist, so accepting one of its
	// candidates hands it to nobody.
	got := consumerFor(t, genesis.SelfCorrectionCandidateRecord{
		Scope: "code", SkillName: "sop-mining", Source: "sop-mining:5e4cce40653d",
		Title: "SOP candidate",
	})
	if got != "none" {
		t.Errorf("staged source has no consumer, got %q", got)
	}
}

func TestConsumerIsNoneForSkillScope(t *testing.T) {
	// coding-dispatch filters scope=code, so every skill candidate is orphaned
	// the moment it leaves the proposed queue.
	got := consumerFor(t, genesis.SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "contract-review", Source: "health-finding:xyz",
		Title: "Recurring failure in contract-review",
	})
	if got != "none" {
		t.Errorf("skill scope has no consumer even on an allowlisted source, got %q", got)
	}
}
