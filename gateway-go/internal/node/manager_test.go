package node

import (
	"testing"
)

func TestRequestAndApprovePairing(t *testing.T) {
	m := NewManager()

	req := m.RequestPairing(PairRequest{
		NodeID:      "node-1",
		DisplayName: "Test Node",
		Platform:    "linux",
	})
	if req.RequestID == "" {
		t.Fatal("expected non-empty requestID")
	}
	if req.State != PairStatePending {
		t.Fatalf("expected pending state, got %s", req.State)
	}

	paired, err := m.ApprovePairing(req.RequestID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paired.NodeID != "node-1" {
		t.Fatalf("expected nodeID 'node-1', got %q", paired.NodeID)
	}
	if paired.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestRejectPairing(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-2"})

	nodeID, err := m.RejectPairing(req.RequestID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nodeID != "node-2" {
		t.Fatalf("expected 'node-2', got %q", nodeID)
	}

	// Can't approve after rejection.
	_, err = m.ApprovePairing(req.RequestID)
	if err == nil {
		t.Fatal("expected error approving rejected request")
	}
}

func TestVerifyToken(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-3"})
	paired, _ := m.ApprovePairing(req.RequestID)

	if !m.VerifyToken("node-3", paired.Token) {
		t.Fatal("expected valid token")
	}
	if m.VerifyToken("node-3", "wrong-token") {
		t.Fatal("expected invalid token")
	}
	if m.VerifyToken("unknown-node", "any") {
		t.Fatal("expected invalid for unknown node")
	}
}

func TestListPairRequests(t *testing.T) {
	m := NewManager()
	m.RequestPairing(PairRequest{NodeID: "pending-1"})
	req2 := m.RequestPairing(PairRequest{NodeID: "approved-1"})
	m.ApprovePairing(req2.RequestID)

	pending, paired := m.ListPairRequests()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if len(paired) != 1 {
		t.Fatalf("expected 1 paired, got %d", len(paired))
	}
}

func TestListNodes(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-a", DisplayName: "Node A"})
	m.ApprovePairing(req.RequestID)

	nodes := m.ListNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].NodeID != "node-a" {
		t.Fatalf("expected 'node-a', got %q", nodes[0].NodeID)
	}
	if !nodes[0].Paired {
		t.Fatal("expected paired=true")
	}
}

func TestDescribeNode(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-x", Platform: "darwin"})
	m.ApprovePairing(req.RequestID)

	info := m.DescribeNode("node-x")
	if info == nil {
		t.Fatal("expected node info")
	}
	if info.Platform != "darwin" {
		t.Fatalf("expected 'darwin', got %q", info.Platform)
	}

	if m.DescribeNode("nonexistent") != nil {
		t.Fatal("expected nil for unknown node")
	}
}

func TestRenameNode(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-r", DisplayName: "Old"})
	m.ApprovePairing(req.RequestID)

	if err := m.RenameNode("node-r", "New"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := m.DescribeNode("node-r")
	if info.DisplayName != "New" {
		t.Fatalf("expected 'New', got %q", info.DisplayName)
	}

	if err := m.RenameNode("nonexistent", "x"); err == nil {
		t.Fatal("expected error for unknown node")
	}
}

func TestSetConnected(t *testing.T) {
	m := NewManager()
	req := m.RequestPairing(PairRequest{NodeID: "node-c"})
	m.ApprovePairing(req.RequestID)

	m.SetConnected("node-c", true)
	info := m.DescribeNode("node-c")
	if !info.Connected {
		t.Fatal("expected connected=true")
	}

	m.SetConnected("node-c", false)
	info = m.DescribeNode("node-c")
	if info.Connected {
		t.Fatal("expected connected=false")
	}
}

func TestCanvasCapability(t *testing.T) {
	m := NewManager()
	if m.GetCanvasCapability() != nil {
		t.Fatal("expected nil before refresh")
	}

	cap := m.RefreshCanvasCapability("http://localhost:3100")
	if cap.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if cap.HostURL != "http://localhost:3100" {
		t.Fatalf("expected host URL, got %q", cap.HostURL)
	}
}

func TestPendingActions(t *testing.T) {
	m := NewManager()

	// Enqueue.
	a := m.EnqueueAction("node-1", PendingAction{Command: "status.request"})
	if a.ID == "" {
		t.Fatal("expected non-empty action ID")
	}

	// Pull.
	actions := m.PullActions("node-1")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	// Ack.
	acked, remaining := m.AckActions("node-1", []string{a.ID})
	if len(acked) != 1 {
		t.Fatalf("expected 1 acked, got %d", len(acked))
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
}

func TestDrainActions(t *testing.T) {
	m := NewManager()
	m.EnqueueAction("node-d", PendingAction{Command: "a"})
	m.EnqueueAction("node-d", PendingAction{Command: "b"})
	m.EnqueueAction("node-d", PendingAction{Command: "c"})

	items, hasMore := m.DrainActions("node-d", 2)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !hasMore {
		t.Fatal("expected hasMore=true")
	}

	items, hasMore = m.DrainActions("node-d", 0)
	if len(items) != 1 {
		t.Fatalf("expected 1 remaining item, got %d", len(items))
	}
	if hasMore {
		t.Fatal("expected hasMore=false")
	}
}

func TestInvokeWaiter(t *testing.T) {
	m := NewManager()
	ch := m.RegisterInvokeWaiter("key-1")

	result := &InvokeResult{OK: true, NodeID: "n1", Command: "ping"}
	resolved := m.ResolveInvoke("key-1", result)
	if !resolved {
		t.Fatal("expected resolved=true")
	}

	got := <-ch
	if !got.OK || got.Command != "ping" {
		t.Fatal("unexpected result")
	}

	// Resolve unknown key.
	if m.ResolveInvoke("unknown", result) {
		t.Fatal("expected resolved=false for unknown key")
	}
}
