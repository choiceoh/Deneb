package channel

import (
	"testing"
)

func TestSnapshotStore_Update(t *testing.T) {
	store := NewSnapshotStore()

	store.Update("telegram", AccountSnapshot{
		AccountID: "default",
		Connected: true,
		Running:   true,
		Enabled:   true,
	})

	snap := store.Snapshot()
	if len(snap.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(snap.Channels))
	}
	ch, ok := snap.Channels["telegram"]
	if !ok {
		t.Fatal("telegram channel not found")
	}
	if !ch.Connected {
		t.Error("expected connected=true")
	}
	if !ch.Running {
		t.Error("expected running=true")
	}
}

func TestSnapshotStore_UpdateAccount(t *testing.T) {
	store := NewSnapshotStore()

	store.UpdateAccount("multi", "workspace-1", AccountSnapshot{
		AccountID: "workspace-1",
		Connected: true,
		Name:      "My Workspace",
	})
	store.UpdateAccount("multi", "workspace-2", AccountSnapshot{
		AccountID: "workspace-2",
		Connected: false,
		LastError: "token expired",
	})

	snap := store.Snapshot()
	if len(snap.ChannelAccounts["multi"]) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(snap.ChannelAccounts["multi"]))
	}

	ws1 := snap.ChannelAccounts["multi"]["workspace-1"]
	if ws1.Name != "My Workspace" {
		t.Errorf("workspace-1 name = %q, want %q", ws1.Name, "My Workspace")
	}

	ws2 := snap.ChannelAccounts["multi"]["workspace-2"]
	if ws2.LastError != "token expired" {
		t.Errorf("workspace-2 error = %q, want %q", ws2.LastError, "token expired")
	}
}

func TestSnapshotStore_SnapshotIsCopy(t *testing.T) {
	store := NewSnapshotStore()
	store.Update("telegram", AccountSnapshot{AccountID: "default", Connected: true})

	snap1 := store.Snapshot()
	snap1.Channels["telegram"] = AccountSnapshot{AccountID: "mutated"}

	snap2 := store.Snapshot()
	if snap2.Channels["telegram"].AccountID == "mutated" {
		t.Error("snapshot should be a deep copy, mutation leaked")
	}
}
