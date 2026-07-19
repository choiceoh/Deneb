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

func TestSyncEnvelopeDedupKeepsTombstoneDistinct(t *testing.T) {
	live := SyncEnvelope{StableID: "thread-1", Revision: "event-9", ContentHash: "abc", ObservedAt: 10}
	replay := live
	if err := live.Validate(); err != nil {
		t.Fatalf("valid envelope: %v", err)
	}
	if !live.SameRevision(replay) {
		t.Fatal("identical replay was not deduplicated")
	}
	tombstone := replay
	tombstone.Deleted = true
	if live.SameRevision(tombstone) {
		t.Fatal("delete tombstone deduplicated against the live record")
	}
	changed := replay
	changed.Revision = "event-10"
	if live.SameRevision(changed) {
		t.Fatal("new revision deduplicated")
	}
}
