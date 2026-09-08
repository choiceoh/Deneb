package knowledge

import "testing"

func TestSyncContractRequiresReplayDeletionFreshnessBoundaries(t *testing.T) {
	valid := SyncContract{
		StableID: "message id", Cursor: "event id", ChangeDetection: "content hash",
		DeletionDetection: "tombstone", FreshnessTargetMillis: 60_000, AuthorizationBoundary: "channel ACL",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid contract: %v", err)
	}
	invalid := valid
	invalid.DeletionDetection = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("contract without deletion detection accepted")
	}
}
