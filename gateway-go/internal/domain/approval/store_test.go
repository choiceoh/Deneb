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
		t.Fatalf("got %q, want command 'rm -rf /'", req.Command)
	}
	if req.Decision != nil {
		t.Fatal("expected nil decision on new request")
	}
	if req.ExpiresAtMs <= req.CreatedAtMs {
		t.Fatal("expected expiresAtMs > createdAtMs")
	}
}


func TestGetRequest(t *testing.T) {
	s := NewStore()
	req := s.CreateRequest(CreateRequestParams{Command: "echo hello"})

	got := s.Get(req.ID)
	if got == nil {
		t.Fatal("got nil, want request")
	}
	if got.Command != "echo hello" {
		t.Fatalf("got %q, want command 'echo hello'", got.Command)
	}

	// Get returns a copy.
	got.Command = "modified"
	original := s.Get(req.ID)
	if original.Command != "echo hello" {
		t.Fatal("Get should return a copy, not a reference")
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
		t.Fatalf("got %v, want allow-once", *got.Decision)
	}
	if got.ResolvedAtMs == nil {
		t.Fatal("expected resolvedAtMs to be set")
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
		t.Fatalf("got %d, want version 1", snap.File.Version)
	}

	s.SetGlobalSnapshot(ApprovalsFile{Version: 2, GlobalDeny: []string{"rm"}}, "hash123")
	snap = s.GlobalSnapshot()
	if snap.File.Version != 2 {
		t.Fatalf("got %d, want version 2", snap.File.Version)
	}
	if snap.Hash != "hash123" {
		t.Fatalf("got %q, want hash 'hash123'", snap.Hash)
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
		t.Fatalf("got %d, want 1 removed", removed)
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
