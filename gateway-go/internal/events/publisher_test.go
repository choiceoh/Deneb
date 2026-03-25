package events

import (
	"testing"
)

// mockSnapshotProvider implements SessionSnapshotProvider for testing.
type mockSnapshotProvider struct {
	snapshots map[string]*SessionSnapshot
}

func (m *mockSnapshotProvider) GetSessionSnapshot(key string) *SessionSnapshot {
	if m.snapshots == nil {
		return nil
	}
	return m.snapshots[key]
}

func TestPublisher_PublishSessionMessage(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{
		id: "conn-1", authed: true, role: "operator",
		scopes: []string{ScopeRead, ScopeWrite},
	}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionEvents("conn-1")

	provider := &mockSnapshotProvider{
		snapshots: map[string]*SessionSnapshot{
			"session-1": {SessionKey: "session-1", Status: "running"},
		},
	}
	pub := NewPublisher(b, provider, nil)

	seq := 1
	pub.PublishSessionMessage(TranscriptUpdate{
		SessionKey: "session-1",
		MessageID:  "msg-1",
		MessageSeq: &seq,
		Message:    map[string]any{"role": "user", "content": "hello"},
	})

	// Should receive both session.message and sessions.changed.
	if len(sub.received) < 1 {
		t.Errorf("expected at least 1 event, got %d", len(sub.received))
	}
}

func TestPublisher_PublishAgentEvent_Sequencing(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{
		id: "conn-1", authed: true, role: "operator",
		scopes: []string{ScopeRead},
	}
	b.Subscribe(sub, Filter{})

	pub := NewPublisher(b, nil, nil)

	pub.PublishAgentEvent(AgentEvent{Kind: "tool", RunID: "run-1"})
	pub.PublishAgentEvent(AgentEvent{Kind: "tool", RunID: "run-1"})
	pub.PublishAgentEvent(AgentEvent{Kind: "tool", RunID: "run-2"})

	// Verify sequences increment per runId.
	pub.seqMu.Lock()
	if pub.agentSeq["run-1"] != 2 {
		t.Errorf("expected seq 2 for run-1, got %d", pub.agentSeq["run-1"])
	}
	if pub.agentSeq["run-2"] != 1 {
		t.Errorf("expected seq 1 for run-2, got %d", pub.agentSeq["run-2"])
	}
	pub.seqMu.Unlock()

	pub.CleanupAgentSeq("run-1")
	pub.seqMu.Lock()
	if _, ok := pub.agentSeq["run-1"]; ok {
		t.Error("expected run-1 seq to be cleaned up")
	}
	pub.seqMu.Unlock()
}

func TestPublisher_EmptySessionKeySkipped(t *testing.T) {
	b := NewBroadcaster()
	pub := NewPublisher(b, nil, nil)

	// Should not panic or broadcast.
	pub.PublishSessionMessage(TranscriptUpdate{})
	pub.PublishSessionMessage(TranscriptUpdate{SessionKey: "key", Message: nil})
}
