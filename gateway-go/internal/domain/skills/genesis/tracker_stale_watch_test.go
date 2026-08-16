package genesis

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// Time-based watch resolution: the label pipeline must not starve on
// rarely-used skills (backtest 2026-07-11: zero historical resolutions).
func TestResolveStaleWatchesConfirmsUsedExpiresUnusedLeavesFreshWatchesAlone(t *testing.T) {
	newTracker := func(t *testing.T) *Tracker {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		tr.SetRollback(func(string) bool { return true }, 3)
		return tr
	}
	use := func(t *testing.T, tr *Tracker, skill string, ok bool) {
		t.Helper()
		if err := tr.RecordUsage(UsageRecord{SkillName: skill, SessionKey: "client:t", Success: ok, ErrorMsg: map[bool]string{true: "", false: "boom"}[ok]}); err != nil {
			t.Fatal(err)
		}
	}
	entryType := func(t *testing.T, tr *Tracker, skill string) string {
		t.Helper()
		entries, err := tr.RecentLifecycleLog(20)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.SkillName == skill && (e.Type == "evolve_confirmed" || e.Type == "evolve_watch_expired" || e.Type == "evolve_rolled_back") {
				return e.Type
			}
		}
		return ""
	}
	backdate := func(t *testing.T, tr *Tracker, skill string, age time.Duration) {
		t.Helper()
		tr.mu.Lock()
		if w := tr.postEvolve[skill]; w != nil {
			w.createdAt = time.Now().Add(-age).UnixMilli()
		}
		tr.mu.Unlock()
	}

	t.Run("stale watch with clean uses confirms and carries the e-process label", func(t *testing.T) {
		tr := newTracker(t)
		use(t, tr, "sk", true) // baseline
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		use(t, tr, "sk", true) // one clean post-evolve use — below the 6-use window
		backdate(t, tr, "sk", 15*24*time.Hour)

		if n := tr.resolveStaleWatches(14 * 24 * time.Hour); n != 1 {
			t.Fatalf("resolved = %d, want 1", n)
		}
		// The confirm callback runs in the calling goroutine here (confirmEvolve
		// is synchronous from ResolveStaleWatches).
		if got := entryType(t, tr, "sk"); got != "evolve_confirmed" {
			t.Fatalf("lifecycle type = %q, want evolve_confirmed", got)
		}
		entries, _ := tr.RecentLifecycleLog(10)
		for _, e := range entries {
			if e.Type == "evolve_confirmed" && e.SkillName == "sk" {
				if e.BaselineTest == nil || e.BaselineTest.N != 1 {
					t.Fatalf("time-based confirm lost the observation-mode label: %+v", e.BaselineTest)
				}
			}
		}
	})

	t.Run("stale watch with zero uses expires without polluting stats", func(t *testing.T) {
		tr := newTracker(t)
		if err := tr.LogEvolveWithAudit("sk0", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		backdate(t, tr, "sk0", 15*24*time.Hour)
		if n := tr.resolveStaleWatches(14 * 24 * time.Hour); n != 1 {
			t.Fatalf("resolved = %d, want 1", n)
		}
		if got := entryType(t, tr, "sk0"); got != "evolve_watch_expired" {
			t.Fatalf("lifecycle type = %q, want evolve_watch_expired", got)
		}
		h := tr.EvolutionHealth()
		if h.EvolveConfirmed7d != 0 || h.EvolveRolledBack7d != 0 {
			t.Fatalf("expiry polluted health stats: %+v", h)
		}
	})

	t.Run("fresh watch is untouched", func(t *testing.T) {
		tr := newTracker(t)
		if err := tr.LogEvolveWithAudit("skf", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		use(t, tr, "skf", true)
		if n := tr.resolveStaleWatches(14 * 24 * time.Hour); n != 0 {
			t.Fatalf("fresh watch resolved: %d", n)
		}
		tr.mu.Lock()
		_, alive := tr.postEvolve["skf"]
		tr.mu.Unlock()
		if !alive {
			t.Fatal("fresh watch removed")
		}
	})

	t.Run("restored pre-timestamp watch starts its clock at restore", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("DENEB_STATE_DIR", filepath.Join(home, ".deneb"))
		tr1, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		tr1.SetRollback(func(string) bool { return true }, 3)
		if err := tr1.LogEvolveWithAudit("skl", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		// Simulate an old binary's persistence: strip the timestamp.
		tr1.mu.Lock()
		tr1.postEvolve["skl"].createdAt = 0
		tr1.saveWatchesLocked()
		tr1.mu.Unlock()

		tr2, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		tr2.SetRollback(func(string) bool { return true }, 3)
		tr2.mu.Lock()
		created := int64(0)
		if w := tr2.postEvolve["skl"]; w != nil {
			created = w.createdAt
		}
		tr2.mu.Unlock()
		if created == 0 {
			t.Fatal("restored watch clock not backfilled — would never expire")
		}
	})
}
