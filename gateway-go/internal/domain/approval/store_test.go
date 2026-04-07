package approval

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestCreateRequest(t *testing.T) {
	s := NewStore()

	req := s.CreateRequest(CreateRequestParams{
		Command: "rm -rf /",
		Ask:     "Delete everything?",
	})

	if req.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if req.Command != "rm -rf /" {
		t.Fatalf("expected command 'rm -rf /', got %q", req.Command)
	}
	if req.Decision != nil {
		t.Fatal("expected nil decision on new request")
	}
	if req.ExpiresAtMs <= req.CreatedAtMs {
		t.Fatal("expected expiresAtMs > createdAtMs")
	}
}

func TestCreateRequestWithCustomID(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{
		ID:      "custom-id",
		Command: "ls",
	})
	if req.ID != "custom-id" {
		t.Fatalf("expected ID 'custom-id', got %q", req.ID)
	}
}

func TestGetRequest(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "echo hello"})

	got := s.Get(req.ID)
	if got == nil {
		t.Fatal("expected request, got nil")
	}
	if got.Command != "echo hello" {
		t.Fatalf("expected command 'echo hello', got %q", got.Command)
	}

	// Get returns a copy.
	got.Command = "modified"
	original := s.Get(req.ID)
	if original.Command != "echo hello" {
		t.Fatal("Get should return a copy, not a reference")
	}
}

func TestGetNotFound(t *testing.T) {
	s := NewStore()
	if s.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent ID")
	}
}

func TestResolve(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "test"})

	err := s.Resolve(req.ID, DecisionAllowOnce)
	testutil.NoError(t, err)

	got := s.Get(req.ID)
	if got.Decision == nil {
		t.Fatal("expected decision to be set")
	}
	if *got.Decision != DecisionAllowOnce {
		t.Fatalf("expected allow-once, got %v", *got.Decision)
	}
	if got.ResolvedAtMs == nil {
		t.Fatal("expected resolvedAtMs to be set")
	}
}

func TestResolveNotFound(t *testing.T) {
	s := NewStore()
	err := s.Resolve("nonexistent", DecisionDeny)
	if err == nil {
		t.Fatal("expected error for nonexistent request")
	}
}

func TestResolveAlreadyResolved(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "test"})

	_ = s.Resolve(req.ID, DecisionDeny)
	err := s.Resolve(req.ID, DecisionAllowAlways)
	if err == nil {
		t.Fatal("expected error for already-resolved request")
	}
}

func TestWaitForDecision(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "test"})

	ch := s.WaitForDecision(req.ID)

	// Resolve in a goroutine.
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = s.Resolve(req.ID, DecisionAllowOnce)
	}()

	select {
	case <-ch:
		got := s.Get(req.ID)
		if got.Decision == nil || *got.Decision != DecisionAllowOnce {
			t.Fatal("expected allow-once decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for decision")
	}
}

func TestWaitForDecisionAlreadyResolved(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "test"})
	_ = s.Resolve(req.ID, DecisionDeny)

	ch := s.WaitForDecision(req.ID)
	select {
	case <-ch:
		// Should return immediately.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WaitForDecision should return immediately for resolved requests")
	}
}

func TestGlobalSnapshot(t *testing.T) {
	s := NewStore()

	snap := s.GlobalSnapshot()
	if snap == nil {
		t.Fatal("expected default global snapshot")
	}
	if snap.File.Version != 1 {
		t.Fatalf("expected version 1, got %d", snap.File.Version)
	}

	s.SetGlobalSnapshot(ApprovalsFile{Version: 2, GlobalDeny: []string{"rm"}}, "hash123")
	snap = s.GlobalSnapshot()
	if snap.File.Version != 2 {
		t.Fatalf("expected version 2, got %d", snap.File.Version)
	}
	if snap.Hash != "hash123" {
		t.Fatalf("expected hash 'hash123', got %q", snap.Hash)
	}
}

func TestCleanup(t *testing.T) {
	s := NewStore()
	// Create with very short TTL.
	s.defaultTTL = 1 * time.Millisecond
	s.CreateRequest(CreateRequestParams{Command: "expired"})

	time.Sleep(5 * time.Millisecond)
	removed := s.Cleanup()
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
}

func TestTwoPhase(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{
		Command:  "dangerous",
		TwoPhase: true,
	})
	if !req.TwoPhase {
		t.Fatal("expected twoPhase=true")
	}
}
