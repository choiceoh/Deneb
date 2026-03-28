package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
)

func TestNewSubagentCommandDepsFromACP(t *testing.T) {
	reg := acp.NewACPRegistry()
	now := time.Now().UnixMilli()

	// Register some agents.
	reg.Register(acp.ACPAgent{
		ID:         "agent-1",
		ParentID:   "session:main",
		Role:       "researcher",
		Status:     "running",
		SessionKey: "session:sub:1",
		SpawnedAt:  now - 60000,
		Depth:      1,
	})
	reg.Register(acp.ACPAgent{
		ID:         "agent-2",
		ParentID:   "session:main",
		Role:       "coder",
		Status:     "done",
		SessionKey: "session:sub:2",
		SpawnedAt:  now - 120000,
		EndedAt:    now - 30000,
		Depth:      1,
	})
	reg.Register(acp.ACPAgent{
		ID:         "agent-3",
		ParentID:   "session:other",
		Role:       "unrelated",
		Status:     "running",
		SessionKey: "session:sub:3",
		SpawnedAt:  now - 10000,
		Depth:      1,
	})

	deps := NewSubagentCommandDepsFromACP(reg)

	// ListRuns should return only agents parented by session:main.
	runs := deps.ListRuns("session:main")
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs for session:main, got %d", len(runs))
	}
	// Active (agent-1) should be first.
	if runs[0].RunID != "agent-1" {
		t.Errorf("expected first run=agent-1, got %s", runs[0].RunID)
	}
	if runs[0].Label != "researcher" {
		t.Errorf("expected label=researcher, got %s", runs[0].Label)
	}
	if runs[1].RunID != "agent-2" {
		t.Errorf("expected second run=agent-2, got %s", runs[1].RunID)
	}

	// Kill should work.
	killed, err := deps.Kill.KillRun("agent-1")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !killed {
		t.Error("expected agent-1 to be killed")
	}
	a := reg.Get("agent-1")
	if a == nil || a.Status != "killed" {
		t.Error("expected agent-1 status=killed in registry")
	}

	// KillAll for session:main (agent-1 already killed, agent-2 already done).
	reg.Register(acp.ACPAgent{
		ID: "agent-4", ParentID: "session:main", Status: "running",
		SessionKey: "session:sub:4", SpawnedAt: now,
	})
	runs2 := deps.ListRuns("session:main")
	killCount, err := deps.Kill.KillAll("session:main", runs2)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if killCount != 1 { // only agent-4 was running
		t.Errorf("expected killAll=1, got %d", killCount)
	}
}

func TestACPSubagentCommandHandler_Handle(t *testing.T) {
	reg := acp.NewACPRegistry()
	now := time.Now().UnixMilli()
	reg.Register(acp.ACPAgent{
		ID: "run-abc", ParentID: "session:main", Role: "worker",
		Status: "running", SessionKey: "session:sub:w", SpawnedAt: now, Depth: 1,
	})

	handler := NewACPSubagentCommandHandler(reg)

	// /subagents list should work.
	result := handler.Handle("/subagents list", "session:main", "telegram", "acc", "", "sender", false, true)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(result.Reply, "worker") {
		t.Errorf("expected 'worker' in list reply, got: %s", result.Reply)
	}

	// /kill should work.
	result = handler.Handle("/kill 1", "session:main", "telegram", "acc", "", "sender", false, true)
	if result == nil {
		t.Fatal("expected non-nil result for /kill")
	}
	// Verify the agent was killed.
	a := reg.Get("run-abc")
	if a == nil || a.Status != "killed" {
		t.Error("expected agent to be killed after /kill 1")
	}

	// Non-subagent command returns nil.
	result = handler.Handle("hello world", "session:main", "telegram", "acc", "", "sender", false, true)
	if result != nil {
		t.Errorf("expected nil for non-command, got: %+v", result)
	}
}

func TestFormatACPSubagentSummary(t *testing.T) {
	reg := acp.NewACPRegistry()
	now := time.Now().UnixMilli()

	// Empty registry.
	if s := FormatACPSubagentSummary(reg); s != "" {
		t.Errorf("expected empty summary, got: %s", s)
	}

	reg.Register(acp.ACPAgent{ID: "a", Status: "running", SpawnedAt: now})
	reg.Register(acp.ACPAgent{ID: "b", Status: "done", SpawnedAt: now, EndedAt: now})
	reg.Register(acp.ACPAgent{ID: "c", Status: "failed", SpawnedAt: now, EndedAt: now})

	s := FormatACPSubagentSummary(reg)
	if !strings.Contains(s, "1 active") || !strings.Contains(s, "1 done") || !strings.Contains(s, "1 failed") {
		t.Errorf("unexpected summary: %s", s)
	}
}

func TestPruneStaleACPAgents(t *testing.T) {
	reg := acp.NewACPRegistry()
	now := time.Now().UnixMilli()

	reg.Register(acp.ACPAgent{ID: "old", Status: "done", SpawnedAt: now - 300000, EndedAt: now - 200000})
	reg.Register(acp.ACPAgent{ID: "recent", Status: "done", SpawnedAt: now - 10000, EndedAt: now - 5000})
	reg.Register(acp.ACPAgent{ID: "running", Status: "running", SpawnedAt: now})

	pruned := PruneStaleACPAgents(reg, 60000) // 1 minute max age
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if reg.Get("old") != nil {
		t.Error("expected 'old' to be pruned")
	}
	if reg.Get("recent") == nil {
		t.Error("expected 'recent' to still exist")
	}
	if reg.Get("running") == nil {
		t.Error("expected 'running' to still exist")
	}
}
