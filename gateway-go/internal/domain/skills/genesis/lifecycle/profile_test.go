package lifecycle

import "testing"

func TestProfilesShareOneLifecycleAndKeepGovernanceFrozen(t *testing.T) {
	profiles := Profiles()
	if len(profiles) != 5 {
		t.Fatalf("profile count = %d, want 5", len(profiles))
	}
	seen := make(map[Layer]bool, len(profiles))
	for _, profile := range profiles {
		if profile.Layer == "" || profile.Title == "" || profile.Detail == "" || profile.ChangeKind == "" {
			t.Fatalf("incomplete profile: %+v", profile)
		}
		if seen[profile.Layer] {
			t.Fatalf("duplicate layer profile: %s", profile.Layer)
		}
		seen[profile.Layer] = true
	}
	governance, ok := ProfileFor(LayerL5)
	if !ok || !governance.Frozen || governance.AutomaticExecution {
		t.Fatalf("L5 policy = %+v, want frozen and non-automatic", governance)
	}
	stages := Stages()
	if len(stages) != 3 || stages[0] != StageObservePropose || stages[2] != StageVerifyLearn {
		t.Fatalf("shared lifecycle = %+v", stages)
	}
}
