package genesis

import (
	"log/slog"
	"testing"
	"time"
)

// Observation mode: the baseline-aware verdict rides every resolving
// lifecycle entry with a Disagreement label, while the legacy threshold
// still owns the firing decision.
func TestRollback_BaselineTestObservation(t *testing.T) {
	use := func(tr *Tracker, skill string, ok bool) {
		t.Helper()
		if err := tr.RecordUsage(UsageRecord{SkillName: skill, SessionKey: "client:t", Success: ok, ErrorMsg: map[bool]string{true: "", false: "boom"}[ok]}); err != nil {
			t.Fatal(err)
		}
	}
	entryFor := func(tr *Tracker, skill, typ string) *LifecycleLogEntry {
		t.Helper()
		entries, err := tr.RecentLifecycleLog(50)
		if err != nil {
			t.Fatal(err)
		}
		for i := range entries {
			if entries[i].SkillName == skill && entries[i].Type == typ {
				return &entries[i]
			}
		}
		return nil
	}

	t.Run("agreement: healthy baseline then hard regression", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		// e-process needs ~8 consecutive fails vs floor baseline to cross
		// 1/alpha (1.475^8 ≈ 22 ≥ 20) — set the legacy threshold there so
		// both trackers resolve on the same evidence.
		tr.SetRollback(func(s string) bool {
			_ = tr.logEvolveRolledBack(s) // evolver callback does this in production
			fired <- s                    // signal only after the entry is durable
			return true
		}, 8)
		for i := 0; i < 10; i++ { // baseline: 10 uses, 0 fails → p0 clamps to floor
			use(tr, "sk", true)
		}
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			use(tr, "sk", false)
		}
		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("rollback did not fire")
		}
		e := entryFor(tr, "sk", "evolve_rolled_back")
		if e == nil || e.BaselineTest == nil {
			t.Fatalf("rolled_back entry missing baseline test: %+v", e)
		}
		if !e.BaselineTest.Reject || e.BaselineTest.Disagreement {
			t.Fatalf("hard regression should agree (reject=true): %+v", e.BaselineTest)
		}
		if e.BaselineTest.N != 8 {
			t.Fatalf("test observed %d uses, want 8", e.BaselineTest.N)
		}
	})

	t.Run("disagreement: noisy baseline where threshold overfires", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		tr.SetRollback(func(s string) bool {
			_ = tr.logEvolveRolledBack(s)
			fired <- s // signal only after the entry is durable
			return true
		}, 3)
		for i := 0; i < 10; i++ { // baseline: 50% fail rate — 3 fails is business as usual
			use(tr, "sk", i%2 == 0)
		}
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		use(tr, "sk", false)
		use(tr, "sk", false)
		use(tr, "sk", false) // legacy threshold fires anyway (baseline-blind)
		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("legacy rollback must still fire in observation mode")
		}
		e := entryFor(tr, "sk", "evolve_rolled_back")
		if e == nil || e.BaselineTest == nil {
			t.Fatalf("rolled_back entry missing baseline test: %+v", e)
		}
		if e.BaselineTest.Reject || !e.BaselineTest.Disagreement {
			t.Fatalf("noisy-baseline overfire must be labeled disagreement: %+v", e.BaselineTest)
		}
	})
}
