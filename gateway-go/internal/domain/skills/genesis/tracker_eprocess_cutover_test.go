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
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		// Legacy threshold parked at 99: a firing can only come from the
		// e-process (clean baseline → p0 clamps to floor; ~8 consecutive
		// fails cross 1/alpha).
		tr.SetRollback(func(s string) bool {
			_ = tr.logEvolveRolledBack(s)
			fired <- s
			return true
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
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan string, 1)
		tr.SetRollback(func(s string) bool {
			_ = tr.logEvolveRolledBack(s)
			fired <- s
			return true
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
		// The watch resolves as a confirm at the C1-extended window (the
		// e-process's MinRejectObservations for a 0.5 baseline, > 2×threshold),
		// carrying the threshold-vs-test disagreement label.
		for i := 0; i < 20; i++ {
			if e := entryFor(tr, "sk", "evolve_confirmed"); e != nil {
				break
			}
			use(tr, "sk", true)
		}
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
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	stash := func(skill string, disagree bool) {
		t.Helper()
		tr.mu.Lock()
		tr.pendingBaselineTest[skill] = &rollbackBaselineTest{Reject: !disagree, RejectReachable: true, Disagreement: disagree}
		tr.mu.Unlock()
		if err := tr.logEvolveRolledBack(skill); err != nil {
			t.Fatal(err)
		}
	}

	if r := tr.eProcessCutoverReadiness(); r.Labels != 0 || r.Ready {
		t.Fatalf("fresh ledger must read 0 labels, not ready: %+v", r)
	}

	// Degenerate labels (recorded while rejection was mathematically
	// unreachable — incl. every pre-C1-fix ledger line, which lacks the
	// field) must not count toward readiness in either direction.
	tr.mu.Lock()
	tr.pendingBaselineTest["sk"] = &rollbackBaselineTest{Disagreement: true}
	tr.mu.Unlock()
	if err := tr.logEvolveRolledBack("sk"); err != nil {
		t.Fatal(err)
	}
	if r := tr.eProcessCutoverReadiness(); r.Labels != 0 || r.UnfairLabels != 1 {
		t.Fatalf("unreachable-reject label must be excluded but audited: %+v", r)
	}

	// 19 agreeing labels: below the n floor even at 100% agreement.
	for i := 0; i < 19; i++ {
		stash("sk", false)
	}
	if r := tr.eProcessCutoverReadiness(); r.Labels != 19 || r.Ready {
		t.Fatalf("n=19 must not be ready: %+v", r)
	}

	// 20th label agrees → n=20, agreement 100% → ready.
	stash("sk", false)
	r := tr.eProcessCutoverReadiness()
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
	if r := tr.eProcessCutoverReadiness(); r.Ready || r.Disagreements != 3 {
		t.Fatalf("87%% agreement must not be ready: %+v", r)
	}

	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
	if r := tr.eProcessCutoverReadiness(); !r.EProcessOwner {
		t.Fatalf("owner flag must mirror the env knob: %+v", r)
	}
}

// C1-D1 guard: a pure-CONFIRM population (no rollback labels) is agreement-
// biased ~1.0 by construction — the legacy owner fires rollbacks before the
// e-process can reject, so the only fair labels are long-survived confirms.
// Readiness must NOT green on that: it requires >=1 fair rollback label so
// agreement was measured against a case the mechanisms could disagree on.
func TestEProcessCutoverReadiness_PureConfirmsNotReady(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// 25 fair, agreeing CONFIRM labels (no rollbacks).
	for i := 0; i < 25; i++ {
		tr.mu.Lock()
		tr.pendingBaselineTest["sk"] = &rollbackBaselineTest{Reject: false, RejectReachable: true, Disagreement: false}
		tr.mu.Unlock()
		if err := tr.logEvolveConfirmed("sk", HarnessEditAudit{}, true); err != nil {
			t.Fatal(err)
		}
	}
	r := tr.eProcessCutoverReadiness()
	if r.Labels < 20 || r.AgreementRate < 0.9 {
		t.Fatalf("precondition: expected n>=20 agreeing confirms: %+v", r)
	}
	if r.FairRollbacks != 0 {
		t.Fatalf("confirm-only population must have 0 fair rollbacks: %+v", r)
	}
	if r.Ready {
		t.Fatalf("pure-confirm agreement must NOT read ready (C1-D1): %+v", r)
	}
	// One fair rollback label unlocks readiness (agreement now tested against a
	// disagreement-capable case).
	tr.mu.Lock()
	tr.pendingBaselineTest["sk"] = &rollbackBaselineTest{Reject: true, RejectReachable: true, Disagreement: false}
	tr.mu.Unlock()
	if err := tr.logEvolveRolledBack("sk"); err != nil {
		t.Fatal(err)
	}
	if r := tr.eProcessCutoverReadiness(); !r.Ready || r.FairRollbacks != 1 {
		t.Fatalf("one fair rollback + n>=20 agreeing must be ready: %+v", r)
	}
}

// C1 regression pin: at the PRODUCTION rollback threshold (3), a hard
// regression must still fire under e-process ownership. Before the confirm
// window was extended to MinRejectObservations, the 6-use window closed at
// E≈10.4 < 20 and Reject() was unreachable — the cutover flip would have
// silently disabled rollback.
func TestRollback_EProcessOwnership_ProductionThreshold(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan string, 1)
	tr.SetRollback(func(s string) bool {
		_ = tr.logEvolveRolledBack(s)
		fired <- s
		return true
	}, DefaultRollbackThreshold)
	if err := tr.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
		t.Fatal(err)
	}
	// Clean (empty) baseline clamps to the floor: fastest rejection path is 8
	// consecutive failures. Feed 10 and require the fire.
	for i := 0; i < 10; i++ {
		if err := tr.RecordUsage(UsageRecord{SkillName: "sk", SessionKey: "client:t", Success: false, ErrorMsg: "boom"}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("production-threshold e-process owner never fired: confirm window still preempts rejection (C1)")
	}
}
