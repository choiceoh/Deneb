package genesis

import "testing"

// The split's whole purpose: a failure where none of the skill's declared tools
// ran is not evidence about the skill body, and must not read as if it were.
func TestUsageQualitySeparatesUnexercisedFromExercisedFailures(t *testing.T) {
	tr := newTestTracker(t)
	records := []UsageRecord{
		{
			SkillName: "kb", Source: UsageSourceReal, Success: false, ErrorMsg: "run failed: tool web errored",
			Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedNo,
		},
		{
			SkillName: "kb", Source: UsageSourceReal, Success: false, ErrorMsg: "run failed: tool wiki errored",
			Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedYes,
		},
		{
			SkillName: "kb", Source: UsageSourceReal, Success: false, ErrorMsg: "run failed: no result",
			Delivery: UsageDeliveryModelRead, Exercised: UsageExercisedUnknown,
		},
		{SkillName: "kb", Source: UsageSourceReal, Success: true, Delivery: UsageDeliveryAutoLoad, Exercised: UsageExercisedYes},
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
	l := got.FailureLayers
	if l.UnexercisedFailures != 1 || l.ExercisedFailures != 1 || l.UnattributableFailures != 1 {
		t.Errorf("layer split = %+v, want 1/1/1", l)
	}
	if l.AutoLoadFailures != 2 || l.ModelReadFailures != 1 {
		t.Errorf("delivery split = auto %d / read %d, want 2/1", l.AutoLoadFailures, l.ModelReadFailures)
	}
}

// Records written before attribution existed carry neither field. They must
// land in "unattributable" — unknown, never silently counted as innocent or
// as procedure failure.
func TestUsageQualityTreatsLegacyRecordsAsUnattributable(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordUsage(UsageRecord{
		SkillName: "kb", Source: UsageSourceReal, Success: false, ErrorMsg: "run failed: tool web errored",
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.FailureLayers.UnattributableFailures != 1 {
		t.Errorf("legacy failure = %+v, want 1 unattributable", got.FailureLayers)
	}
	if got.FailureLayers.UnexercisedFailures != 0 || got.FailureLayers.ExercisedFailures != 0 {
		t.Errorf("legacy record leaked into an attributed bucket: %+v", got.FailureLayers)
	}
}

// The split reports on the SAME population the success rate counts. A record
// the quality filter ignores must not appear in the layers either, or the two
// numbers describe different things.
func TestUsageQualityLayersIgnoreFilteredRecords(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordUsage(UsageRecord{
		SkillName: "kb", Source: UsageSourceReviewVerdict, Success: false, ErrorMsg: "run failed",
		Exercised: UsageExercisedNo,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.FailureLayers != (SkillFailureLayers{}) {
		t.Errorf("review-fork record entered the layer split: %+v", got.FailureLayers)
	}
}

// The workout lane re-records the same held-out replay failure every cycle.
// Those synthetic records are evidence for clustering at most (isRealUsageRecord
// excludes them from the success rate), so counting them here would steadily
// inflate the failure layers and pass synthetic drift off as real-use evidence.
func TestUsageQualityIgnoresSyntheticWorkoutRecords(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordUsage(UsageRecord{
		SkillName: "kb", Source: usageSourceWorkout, Success: false,
		ErrorMsg: "held-out replay assertion failure",
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	got, err := tr.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.CountedRecords != 0 || got.FailureLayers != (SkillFailureLayers{}) {
		t.Errorf("workout record counted as real use: counted=%d layers=%+v", got.CountedRecords, got.FailureLayers)
	}
	if got.IgnoredSyntheticRecords != 1 {
		t.Errorf("IgnoredSyntheticRecords = %d, want 1", got.IgnoredSyntheticRecords)
	}
}
