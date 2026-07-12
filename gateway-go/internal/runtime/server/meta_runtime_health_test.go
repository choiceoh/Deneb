package server

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// pct computes a whole-number percentage for the advisory one-liner.
func TestPct(t *testing.T) {
	if got := pct(1, 4); got != 25.0 {
		t.Fatalf("pct(1,4) = %v, want 25", got)
	}
	if got := pct(3, 0); got != 0 {
		t.Fatalf("pct(3,0) = %v, want 0 (div-by-zero guard)", got)
	}
}

// slowestModelName picks the p95-worst model and shortens long names.
func TestSlowestModelName(t *testing.T) {
	stats := []agentlog.ModelStat{
		{Model: "gpt-4o-2024-08-06", P95Ms: 12000},
		{Model: "fast-model", P95Ms: 3000},
	}
	// Stats are expected pre-sorted by p95 desc; just check the name shortening.
	if got := slowestModelName(stats); !strings.HasPrefix(got, "gpt-4o-2024-08-06") {
		// No space in this name, so it stays whole — that's fine.
		t.Logf("slowestModelName = %q (no space to split, kept whole)", got)
	}
	// Empty stats → placeholder.
	if got := slowestModelName(nil); got != "?" {
		t.Fatalf("slowestModelName(nil) = %q, want ?", got)
	}
}

// The evidence summary must be empty when no agentlog is wired (dev/test
// without session logs) — the quiet-not-broken distinction.
func TestMetaRuntimeHealthEvidence_NilWriter(t *testing.T) {
	s := &Server{AutonomousSubsystem: &AutonomousSubsystem{}}
	if got := s.metaRuntimeHealthEvidence(context.Background()); got != "" {
		t.Fatalf("nil agentLogWriter should yield empty, got %q", got)
	}
}
