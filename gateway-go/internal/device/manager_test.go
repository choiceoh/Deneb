package device

import (
	"testing"
)

func TestAddAndApprovePair(t *testing.T) {
	m := NewManager()
	entry := m.AddPairRequest("dev-1", "Phone", "ios")

	if entry.RequestID == "" {
		t.Fatal("expected non-empty requestID")
	}
	if entry.State != PairStatePending {
		t.Fatalf("expected pending, got %s", entry.State)
	}

	dev, err := m.Approve(entry.RequestID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dev.DeviceID != "dev-1" {
		t.Fatalf("expected dev-1, got %q", dev.DeviceID)
	}
	if dev.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestRejectPair(t *testing.T) {
	m := NewManager()
	entry := m.AddPairRequest("dev-2", "Tablet", "android")

	if err := m.Reject(entry.RequestID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Can't approve after rejection.
	_, err := m.Approve(entry.RequestID)
	if err == nil {
		t.Fatal("expected error approving rejected request")
	}
}

func TestListPairs(t *testing.T) {
	m := NewManager()
	m.AddPairRequest("d1", "A", "x")
	m.AddPairRequest("d2", "B", "y")

	pairs := m.ListPairs()
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
}

func TestRemoveDevice(t *testing.T) {
	m := NewManager()
	entry := m.AddPairRequest("d1", "Test", "x")
	m.Approve(entry.RequestID)

	if err := m.Remove("d1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Remove("d1"); err == nil {
		t.Fatal("expected error removing already-removed device")
	}
}

func TestRotateToken(t *testing.T) {
	m := NewManager()
	entry := m.AddPairRequest("d1", "Test", "x")
	dev, _ := m.Approve(entry.RequestID)
	oldToken := dev.Token

	newToken, err := m.RotateToken("d1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("expected new token to differ from old")
	}

	_, err = m.RotateToken("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown device")
	}
}

func TestRevokeToken(t *testing.T) {
	m := NewManager()
	entry := m.AddPairRequest("d1", "Test", "x")
	m.Approve(entry.RequestID)

	if err := m.RevokeToken("d1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.RevokeToken("nonexistent"); err == nil {
		t.Fatal("expected error for unknown device")
	}
}
