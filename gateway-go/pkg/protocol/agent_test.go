package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestAgentStatusIsTerminal(t *testing.T) {
	tests := []struct {
		status   protocol.AgentStatus
		terminal bool
	}{
		{protocol.AgentStatusUnspecified, false},
		{protocol.AgentStatusSpawning, false},
		{protocol.AgentStatusRunning, false},
		{protocol.AgentStatusCompleted, true},
		{protocol.AgentStatusFailed, true},
		{protocol.AgentStatusKilled, true},
	}
	for _, tc := range tests {
		if got := tc.status.IsTerminal(); got != tc.terminal {
			t.Errorf("AgentStatus(%q).IsTerminal() = %v, want %v", tc.status, got, tc.terminal)
		}
	}
}

func TestAgentSpawnRequestJSON(t *testing.T) {
	model := "claude-4"
	req := protocol.AgentSpawnRequest{
		SessionKey: "sess-123",
		Model:      &model,
	}
	data := testutil.Must(json.Marshal(req))
	var decoded protocol.AgentSpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal AgentSpawnRequest: %v", err)
	}
	if decoded.SessionKey != "sess-123" {
		t.Errorf("SessionKey = %q, want %q", decoded.SessionKey, "sess-123")
	}
	if decoded.Model == nil || *decoded.Model != "claude-4" {
		t.Errorf("Model = %v, want %q", decoded.Model, "claude-4")
	}
	if decoded.Provider != nil {
		t.Errorf("Provider should be nil, got %v", decoded.Provider)
	}
}
