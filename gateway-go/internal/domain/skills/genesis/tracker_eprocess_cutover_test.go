package genesis

import (
	"log/slog"
	"testing"
	"time"
)

// Cutover mode (DENEB_EPROCESS_OWNS_ROLLBACK=1): the anytime-valid e-process
// owns rollback firing; the legacy threshold verdict keeps riding every
// resolving entry as a disagreement label.
func TestRollback_EProcessOwnership(t *testing.T) {
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

	t.Run("e-process fires on hard regression even when legacy threshold is far away", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		// Legacy threshold parked at 99: a firing can only come from the
		// e-process (clean baseline → p0 clamps to floor; ~8 consecutive
		// fails cross 1/alpha).
		tr.SetRollback(func(s string) {
			_ = tr.LogEvolveRolledBack(s)
			fired <- s
		}, 99)
		for i := 0; i < 10; i++ { // healthy baseline
			use(tr, "sk", true)
		}
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 12; i++ {
			use(tr, "sk", false)
		}
		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("e-process owner did not fire the rollback on a hard regression")
		}
		e := entryFor(tr, "sk", "evolve_rolled_back")
		if e == nil || e.BaselineTest == nil {
			t.Fatalf("rolled_back entry missing baseline test: %+v", e)
		}
		if !e.BaselineTest.Reject {
			t.Fatalf("owning test must have rejected: %+v", e.BaselineTest)
		}
		// Legacy threshold (99) had NOT fired — that mismatch is exactly the
		// disagreement label that must keep accumulating after cutover.
		if !e.BaselineTest.Disagreement {
			t.Fatalf("threshold-quiet e-process fire must be labeled disagreement: %+v", e.BaselineTest)
		}
	})

	t.Run("noisy baseline: threshold-crossing fails do NOT fire under e-process ownership", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		tr.SetRollback(func(s string) {
			_ = tr.LogEvolveRolledBack(s)
			fired <- s
		}, 3)
		for i := 0; i < 10; i++ { // 50% baseline failure rate — 3 fails is business as usual
			use(tr, "sk", i%2 == 0)
		}
		if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
			t.Fatal(err)
		}
		use(tr, "sk", false)
		use(tr, "sk", false)
		use(tr, "sk", false) // legacy threshold would fire here — e-process must not
		select {
		case s := <-fired:
			t.Fatalf("e-process owner fired a baseline-blind rollback for %q", s)
		case <-time.After(200 * time.Millisecond):
		}
		// The watch resolves at the window (2×threshold = 6 uses) as a
		// confirm, carrying the threshold-vs-test disagreement label.
		use(tr, "sk", true)
		use(tr, "sk", true)
		use(tr, "sk", true)
		deadline := time.Now().Add(2 * time.Second)
		var e *LifecycleLogEntry
		for time.Now().Before(deadline) {
			if e = entryFor(tr, "sk", "evolve_confirmed"); e != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e == nil || e.BaselineTest == nil {
			t.Fatalf("confirmed entry missing baseline test: %+v", e)
		}
		if e.BaselineTest.Reject {
			t.Fatalf("noisy-baseline survivor must not carry a reject verdict: %+v", e.BaselineTest)
		}
		if !e.BaselineTest.Disagreement {
			t.Fatalf("threshold fired (3 fails) while the test stayed quiet — must be a disagreement label: %+v", e.BaselineTest)
		}
	})
}

// Readiness scores the accumulated observation labels against the graduation
// thresholds (n>=20, agreement>=90%).
func TestEProcessCutoverReadiness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	stash := func(skill string, disagree bool) {
		t.Helper()
		tr.mu.Lock()
		tr.pendingBaselineTest[skill] = &RollbackBaselineTest{Reject: !disagree, Disagreement: disagree}
		tr.mu.Unlock()
		if err := tr.LogEvolveRolledBack(skill); err != nil {
			t.Fatal(err)
		}
	}

	if r := tr.EProcessCutoverReadiness(); r.Labels != 0 || r.Ready {
		t.Fatalf("fresh ledger must read 0 labels, not ready: %+v", r)
	}

	// 19 agreeing labels: below the n floor even at 100% agreement.
	for i := 0; i < 19; i++ {
		stash("sk", false)
	}
	if r := tr.EProcessCutoverReadiness(); r.Labels != 19 || r.Ready {
		t.Fatalf("n=19 must not be ready: %+v", r)
	}

	// 20th label agrees → n=20, agreement 100% → ready.
	stash("sk", false)
	r := tr.EProcessCutoverReadiness()
	if r.Labels != 20 || !r.Ready || r.AgreementRate != 1.0 {
		t.Fatalf("n=20 all-agree must be ready: %+v", r)
	}
	if r.EProcessOwner {
		t.Fatalf("owner flag must be off without the env knob: %+v", r)
	}

	// 3 disagreements → 20/23 ≈ 87% < 90% → not ready despite n>=20.
	for i := 0; i < 3; i++ {
		stash("sk", true)
	}
	if r := tr.EProcessCutoverReadiness(); r.Ready || r.Disagreements != 3 {
		t.Fatalf("87%% agreement must not be ready: %+v", r)
	}

	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
	if r := tr.EProcessCutoverReadiness(); !r.EProcessOwner {
		t.Fatalf("owner flag must mirror the env knob: %+v", r)
	}
}
