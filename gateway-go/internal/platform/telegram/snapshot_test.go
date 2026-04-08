package telegram

import "testing"

func TestSnapshotStore_Update(t *testing.T) {
	store := NewSnapshotStore()

	store.Update(AccountSnapshot{
		AccountID: "default",
		Connected: true,
		Running:   true,
		Enabled:   true,
	})

	snap := store.Snapshot()
	ch, ok := snap.Channels["telegram"]
	if !ok {
		t.Fatal("telegram entry not found in snapshot")
	}
	if !ch.Connected {
		t.Error("expected connected=true")
	}
	if !ch.Running {
		t.Error("expected running=true")
	}
}


func TestSnapshotStore_SnapshotIsCopy(t *testing.T) {
	store := NewSnapshotStore()
	store.Update(AccountSnapshot{AccountID: "default", Connected: true})

	snap1 := store.Snapshot()
	snap1.Channels["telegram"] = AccountSnapshot{AccountID: "mutated"}

	snap2 := store.Snapshot()
	if snap2.Channels["telegram"].AccountID == "mutated" {
		t.Error("snapshot should be a deep copy, mutation leaked")
	}
}
