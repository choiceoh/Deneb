package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

// The L1 scoreboard must split proposals the evolver's gates refused at
// proposal time from the rest: "제안 17 · 진화 0" reads as candidate quality
// until the 17 are seen to be the reviewer nominating one gated skill.
func TestEvolutionHealthCountsSuppressedProposals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []EvolutionProposalRecord{
		{Candidate: "tighten the weather paragraph", Route: "evolve", SkillName: "morning-letter", Suppressed: "recency gate: \"morning-letter\" last really used 2026-07-20"},
		{Candidate: "add DR comparison", Route: "evolve", SkillName: "contract-review", Executed: true},
		{Candidate: "", Route: "no-op", SkillName: "weekly-report"},
	} {
		if err := tr.LogEvolutionProposal(rec); err != nil {
			t.Fatal(err)
		}
	}
	h := tr.EvolutionHealth()
	if h.Proposals7d != 3 {
		t.Fatalf("proposals = %d, want 3", h.Proposals7d)
	}
	if h.ProposalsSuppressed7d != 1 {
		t.Fatalf("suppressed proposals = %d, want 1", h.ProposalsSuppressed7d)
	}
	l1 := tr.rsiAssessL1()
	var found bool
	for _, m := range l1.Metrics {
		if m.Label == "제안 중 게이트 억제" && m.Value == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("L1 metrics missing the suppression counter: %+v", l1.Metrics)
	}
	if l1.State != rsiStateDataGated || !strings.Contains(l1.Diagnosis, "억제") {
		t.Fatalf("L1 should read DATA-GATED with the suppression note, got %s / %q", l1.State, l1.Diagnosis)
	}
}
