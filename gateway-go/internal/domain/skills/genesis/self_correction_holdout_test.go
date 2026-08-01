package genesis

import (
	"fmt"
	"testing"
	"time"
)

// The holdout only answers "does the lane pay for itself" if membership is
// stable, bounded, and switchable off. Each property gets its own test — a
// table would stop at the first failure and hide the rest.

func heldOutFixture(id string, age time.Duration) (SelfCorrectionCandidateRecord, time.Time) {
	now := time.Now()
	return SelfCorrectionCandidateRecord{ID: id, CreatedAt: now.Add(-age).UnixMilli()}, now
}

func TestHoldoutMembershipIsStableForTheSameID(t *testing.T) {
	// A candidate that flipped sides between ticks would be neither dispatched
	// promptly nor a clean control.
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "10")
	rec, now := heldOutFixture("sc-1785530691313-adf66d12", time.Hour)
	first := selfCorrectionHeldOut(rec, now)
	for i := 0; i < 50; i++ {
		if selfCorrectionHeldOut(rec, now) != first {
			t.Fatalf("membership must not vary for a fixed id")
		}
	}
}

func TestHoldoutReleasesAfterTheWindow(t *testing.T) {
	// A control group is worth a delay, not an abandoned defect.
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "100")
	fresh, now := heldOutFixture("sc-any", time.Hour)
	if !selfCorrectionHeldOut(fresh, now) {
		t.Fatal("a fresh candidate at 100% must be held")
	}
	old, now2 := heldOutFixture("sc-any", selfCorrectionHoldoutWindow+time.Hour)
	if selfCorrectionHeldOut(old, now2) {
		t.Error("the hold must expire so the defect still gets fixed")
	}
}

func TestHoldoutDisabledAtZero(t *testing.T) {
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "0")
	rec, now := heldOutFixture("sc-any", time.Hour)
	if selfCorrectionHeldOut(rec, now) {
		t.Error("0 percent must withhold nothing")
	}
}

func TestHoldoutPercentIsClampedAndFailsSafe(t *testing.T) {
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "500")
	if got := selfCorrectionHoldoutPercent(); got != 50 {
		t.Errorf("an over-large share must clamp to 50, got %d", got)
	}
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "not-a-number")
	if got := selfCorrectionHoldoutPercent(); got != selfCorrectionHoldoutDefaultPercent {
		t.Errorf("a malformed share must fall back to the default, got %d", got)
	}
}

func TestHoldoutShareIsRoughlyTheConfiguredFraction(t *testing.T) {
	// A biased mixer would make the holdout a sample of mining ORDER rather than
	// a random slice, which would void the comparison.
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "10")
	now := time.Now()
	held := 0
	const n = 2000
	for i := 0; i < n; i++ {
		rec := SelfCorrectionCandidateRecord{
			ID:        fmt.Sprintf("sc-1785530691313-%08x", i),
			CreatedAt: now.Add(-time.Hour).UnixMilli(),
		}
		if selfCorrectionHeldOut(rec, now) {
			held++
		}
	}
	if held < n*6/100 || held > n*15/100 {
		t.Errorf("expected roughly 10%% held, got %d/%d", held, n)
	}
}

func TestHoldoutSkipsRecordsWithoutACreationClock(t *testing.T) {
	// Without a timestamp the hold could never expire; withholding forever is
	// worse than not sampling that record.
	t.Setenv("DENEB_RSI_HOLDOUT_PERCENT", "100")
	if selfCorrectionHeldOut(SelfCorrectionCandidateRecord{ID: "sc-any"}, time.Now()) {
		t.Error("a record with no createdAt must not be withheld")
	}
}
