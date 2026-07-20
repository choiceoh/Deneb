package web

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestTierMemoryLearnsDecaysAndProbes(t *testing.T) {
	tm := newTierMemory("")
	t0 := time.Unix(1_700_000_000, 0)

	if got := tm.startStage("hard.example", t0); got != 0 {
		t.Fatalf("unknown host start = %d, want 0", got)
	}

	tm.recordSuccess("hard.example", 2, t0)
	if got := tm.startStage("hard.example", t0.Add(time.Minute)); got != 2 {
		t.Fatalf("learned start = %d, want 2", got)
	}

	// After the probe interval, exactly one fetch probes a stage lower…
	if got := tm.startStage("hard.example", t0.Add(25*time.Hour)); got != 1 {
		t.Fatalf("probe start = %d, want 1", got)
	}
	// …and an immediate follow-up does not re-probe.
	if got := tm.startStage("hard.example", t0.Add(25*time.Hour+time.Second)); got != 2 {
		t.Fatalf("post-probe start = %d, want 2", got)
	}

	// A success at a lower stage moves the tier down.
	tm.recordSuccess("hard.example", 1, t0.Add(26*time.Hour))
	if got := tm.startStage("hard.example", t0.Add(26*time.Hour+time.Minute)); got != 1 {
		t.Fatalf("lowered start = %d, want 1", got)
	}

	// A stage-0 success drops the entry entirely.
	tm.recordSuccess("hard.example", 0, t0.Add(27*time.Hour))
	if got := tm.startStage("hard.example", t0.Add(27*time.Hour+time.Minute)); got != 0 {
		t.Fatalf("post-stage0 start = %d, want 0", got)
	}

	// TTL expiry resets to 0.
	tm.recordSuccess("stale.example", 3, t0)
	if got := tm.startStage("stale.example", t0.Add(tierTTL+time.Hour)); got != 0 {
		t.Fatalf("expired start = %d, want 0", got)
	}

	// Empty host is a no-op.
	tm.recordSuccess("", 2, t0)
	if got := tm.startStage("", t0); got != 0 {
		t.Fatalf("empty host start = %d, want 0", got)
	}
}

func TestTierMemoryPersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tiers.json")
	t0 := time.Unix(1_700_000_000, 0)

	tm := newTierMemory(path)
	tm.recordSuccess("hard.example", 2, t0)

	reloaded := newTierMemory(path)
	if got := reloaded.startStage("hard.example", t0.Add(time.Minute)); got != 2 {
		t.Fatalf("reloaded start = %d, want 2", got)
	}
}

func TestTierMemoryEvictsOldestBeyondCap(t *testing.T) {
	tm := newTierMemory("")
	t0 := time.Unix(1_700_000_000, 0)
	for i := 0; i < tierMaxEntries+10; i++ {
		tm.recordSuccess(fmt.Sprintf("host%d.example", i), 1, t0.Add(time.Duration(i)*time.Second))
	}
	if len(tm.entries) > tierMaxEntries {
		t.Fatalf("entries = %d, want ≤ %d", len(tm.entries), tierMaxEntries)
	}
	// The earliest hosts are the eviction victims.
	if got := tm.startStage("host0.example", t0.Add(time.Hour)); got != 0 {
		t.Errorf("oldest host survived eviction (start=%d)", got)
	}
	if got := tm.startStage(fmt.Sprintf("host%d.example", tierMaxEntries+9), t0.Add(time.Hour)); got != 1 {
		t.Errorf("newest host evicted (start=%d)", got)
	}
}
