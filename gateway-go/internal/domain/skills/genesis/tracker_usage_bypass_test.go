package genesis

import "testing"

// The split's whole purpose: a SUCCESS in which none of the skill's declared
// procedure ran is not evidence that the skill worked. Without this it is
// indistinguishable from a run the skill actually carried.
func TestUsageQualitySeparatesBypassedFromExercisedSuccesses(t *testing.T) {
	tr := newTestTracker(t)
	records := []UsageRecord{
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedYes},
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo},
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryModelRead, Exercised: UsageExercisedNo},
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryModelRead, Exercised: UsageExercisedUnknown},
	}
	for _, r := range records {
		if err := tr.RecordUsage(r); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}

	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	b := got.Bypass
	if b.ExercisedSuccesses != 1 || b.BypassedSuccesses != 2 || b.UnattributableSuccesses != 1 {
		t.Errorf("bypass split = %+v, want 1 exercised / 2 bypassed / 1 unattributable", b)
	}
	if b.AutoLoadBypasses != 1 || b.ModelReadBypasses != 1 {
		t.Errorf("delivery split = auto %d / read %d, want 1/1", b.AutoLoadBypasses, b.ModelReadBypasses)
	}
	// The denominator excludes the unattributable record rather than assuming
	// it was exercised, which would silently dilute every legacy skill's rate.
	if b.AttributedSuccesses() != 3 {
		t.Errorf("attributed successes = %d, want 3", b.AttributedSuccesses())
	}
}

// The failure split and the bypass split must be exact complements over the
// same counted records: a failure that never exercised the skill belongs to
// UnexercisedFailures, and must not also read as a bypassed success.
func TestUsageQualityBypassAndFailureSplitsAreComplements(t *testing.T) {
	tr := newTestTracker(t)
	records := []UsageRecord{
		{SkillName: "kb", Source: UsageSourceReal, Success: false, ErrorMsg: "run failed: tool web errored", Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo},
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo},
	}
	for _, r := range records {
		if err := tr.RecordUsage(r); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}

	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.FailureLayers.UnexercisedFailures != 1 {
		t.Errorf("failure layers = %+v, want 1 unexercised failure", got.FailureLayers)
	}
	if got.Bypass.BypassedSuccesses != 1 || got.Bypass.ExercisedSuccesses != 0 {
		t.Errorf("bypass split = %+v, want exactly the success counted", got.Bypass)
	}
	failures := got.FailureLayers.UnexercisedFailures + got.FailureLayers.ExercisedFailures + got.FailureLayers.UnattributableFailures
	successes := got.Bypass.AttributedSuccesses() + got.Bypass.UnattributableSuccesses
	if failures+successes != got.CountedRecords {
		t.Errorf("splits cover %d of %d counted records", failures+successes, got.CountedRecords)
	}
}

// Records written before attribution existed carry neither field. A bypass
// verdict on them would be invented evidence, so they must stay unattributable.
func TestUsageQualityBypassTreatsLegacySuccessAsUnattributable(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordUsage(UsageRecord{SkillName: "kb", Source: UsageSourceReal, Success: true}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.Bypass.UnattributableSuccesses != 1 {
		t.Errorf("legacy success = %+v, want 1 unattributable", got.Bypass)
	}
	if got.Bypass.BypassedSuccesses != 0 || got.Bypass.ExercisedSuccesses != 0 {
		t.Errorf("legacy record leaked into an attributed bucket: %+v", got.Bypass)
	}
	if got.Bypass.Actionable() {
		t.Error("a record with no attribution must never raise the advisory")
	}
}

// The review fork and the synthetic lanes replay the same records every cycle.
// Letting them into the split would manufacture a bypass backlog out of
// evidence the evolver already refuses to count.
func TestUsageQualityBypassExcludesReviewAndSyntheticRecords(t *testing.T) {
	tr := newTestTracker(t)
	records := []UsageRecord{
		{SkillName: "kb", Source: UsageSourceReviewVerdict, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo},
		{SkillName: "kb", Source: usageSourceWorkout, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo},
	}
	for _, r := range records {
		if err := tr.RecordUsage(r); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}
	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.Bypass != (SkillBypassSignal{}) {
		t.Errorf("non-real record entered the bypass split: %+v", got.Bypass)
	}
}

// Both floors have to hold. The evidence floor is what stops a single
// documented branch from reading as a stale skill; the rate floor is what
// separates "occasionally incidental" from "no longer earning the turn".
func TestSkillBypassSignalActionableHonorsBothFloors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		exercised int
		bypassed  int
		want      bool
	}{
		{"no evidence", 0, 0, false},
		{"below evidence floor even at 100%", 0, 3, false},
		{"at evidence floor but below rate", 3, 1, false},
		{"at evidence floor and exactly at rate", 2, 2, true},
		{"clear majority bypassed", 2, 6, true},
		{"fully exercised", 8, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := SkillBypassSignal{ExercisedSuccesses: tc.exercised, BypassedSuccesses: tc.bypassed}
			if got := b.Actionable(); got != tc.want {
				t.Fatalf("Actionable() = %v, want %v (rate %.2f over %d attributed)", got, tc.want, b.BypassRate(), b.AttributedSuccesses())
			}
		})
	}
}

// Unattributable successes must not pad the denominator: a skill with four
// bypasses and four unmeasurable runs is still a 100% bypass rate on the
// evidence that exists, not 50%.
func TestSkillBypassRateIgnoresUnattributableSuccesses(t *testing.T) {
	t.Parallel()
	b := SkillBypassSignal{BypassedSuccesses: 4, UnattributableSuccesses: 4}
	if got := b.BypassRate(); got != 1 {
		t.Errorf("BypassRate() = %v, want 1", got)
	}
	if !b.Actionable() {
		t.Error("four attributed bypasses must clear both floors")
	}
}
