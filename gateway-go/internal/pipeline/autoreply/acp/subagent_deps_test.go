package acp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestSubagentCommandDeps_SpawnSubagent(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	result := deps.SpawnSubagent(context.Background(), SpawnSubagentParams{
		ParentSessionKey: "parent-session",
		ParentAgentID:    "parent-agent",
		Role:             "researcher",
		WorkspaceDir:     "/tmp/workspace",
		Model:            "claude-opus-4.6",
		Provider:         "anthropic",
	})

	if result.Error != nil {
		t.Fatalf("SpawnSubagent() error = %v", result.Error)
	}
	if result.AgentID == "" {
		t.Error("expected non-empty agent ID")
	}
	if result.SessionKey == "" {
		t.Error("expected non-empty session key")
	}

	// Agent should be registered.
	agent := acpRegistry.Get(result.AgentID)
	if agent == nil {
		t.Fatal("expected agent in registry")
	}
	if agent.Role != "researcher" {
		t.Errorf("role = %q, want 'researcher'", agent.Role)
	}
	if agent.Status != "idle" {
		t.Errorf("status = %q, want 'idle'", agent.Status)
	}
	if agent.Depth != 0 {
		t.Errorf("depth = %d, want 0 (parent not in registry)", agent.Depth)
	}
}

func TestSubagentCommandDeps_SpawnSubagent_MaxDepth(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	// Register a deep parent.
	acpRegistry.Register(ACPAgent{
		ID:    "deep-parent",
		Depth: 4,
	})

	result := deps.SpawnSubagent(context.Background(), SpawnSubagentParams{
		ParentSessionKey: "session",
		ParentAgentID:    "deep-parent",
		Role:             "child",
		MaxDepth:         5,
	})

	if result.Error == nil {
		t.Error("expected error for max depth exceeded")
	}
}

func TestSubagentCommandDeps_KillSubagent(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	acpRegistry.Register(ACPAgent{
		ID:       "agent-1",
		Status:   "running",
		ParentID: "",
	})
	acpRegistry.Register(ACPAgent{
		ID:       "agent-2",
		Status:   "running",
		ParentID: "agent-1",
	})

	err := deps.KillSubagent("agent-1")
	testutil.NoError(t, err)

	// Both should be killed.
	parent := acpRegistry.Get("agent-1")
	if parent.Status != "killed" {
		t.Errorf("parent status = %q, want 'killed'", parent.Status)
	}
	child := acpRegistry.Get("agent-2")
	if child.Status != "killed" {
		t.Errorf("child status = %q, want 'killed'", child.Status)
	}
}

func TestSubagentCommandDeps_ListSubagents(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	// No agents.
	result := deps.ListSubagents("")
	if result != "No active subagents." {
		t.Errorf("got %q, want 'No active subagents.'", result)
	}

	// Add some agents.
	acpRegistry.Register(ACPAgent{
		ID:       "agent-1",
		Role:     "researcher",
		Status:   "running",
		ParentID: "parent",
	})
	result = deps.ListSubagents("parent")
	if result == "No active subagents." {
		t.Error("expected agent in list")
	}
}

func TestSubagentCommandDeps_SpawnSubagent_MaxBreadth(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	// Register a parent.
	acpRegistry.Register(ACPAgent{ID: "parent", Depth: 0})

	// Spawn 10 children (max allowed).
	for i := range 10 {
		result := deps.SpawnSubagent(context.Background(), SpawnSubagentParams{
			ParentSessionKey: "session",
			ParentAgentID:    "parent",
			Role:             "worker",
		})
		if result.Error != nil {
			t.Fatalf("spawn %d: unexpected error: %v", i, result.Error)
		}
	}

	// 11th should fail due to breadth limit.
	result := deps.SpawnSubagent(context.Background(), SpawnSubagentParams{
		ParentSessionKey: "session",
		ParentAgentID:    "parent",
		Role:             "worker",
	})
	if result.Error == nil {
		t.Error("expected error for max breadth exceeded")
	}
}

func TestSubagentCommandDeps_ResetSubagent_RunningGuard(t *testing.T) {
	acpRegistry := NewACPRegistry()
	deps := &SubagentInfraDeps{
		ACPRegistry: acpRegistry,
	}

	acpRegistry.Register(ACPAgent{
		ID:     "running-agent",
		Status: "running",
	})

	err := deps.ResetSubagent("running-agent", "test reset")
	if err == nil {
		t.Error("expected error when resetting a running agent")
	}

	// Done agent should be resettable.
	acpRegistry.Register(ACPAgent{
		ID:     "done-agent",
		Status: "done",
	})
	err = deps.ResetSubagent("done-agent", "test reset")
	testutil.NoError(t, err)

	agent := acpRegistry.Get("done-agent")
	if agent.Status != "idle" {
		t.Errorf("status after reset = %q, want 'idle'", agent.Status)
	}
}

