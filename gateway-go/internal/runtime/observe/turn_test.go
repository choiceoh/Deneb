package observe

import (
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
)

func TestBuildTurnView_JoinsAgentLogAndRing(t *testing.T) {
	dir := t.TempDir()
	w := agentlog.NewWriter(dir)
	rl := agentlog.NewRunLogger(w, "client:main", "run-xyz")
	rl.LogStart(agentlog.RunStartData{Model: "opus", Message: "hi"})
	rl.LogTurnTool(agentlog.TurnToolData{Turn: 1, Name: "grep", DurationMs: 12})
	rl.LogEnd(agentlog.RunEndData{StopReason: "end_turn", Turns: 1, OutputTokens: 50})

	ring := NewRing(10)
	lc := NewCapture(slog.NewTextHandler(io.Discard, nil), ring)
	slog.New(lc).With("runId", "run-xyz", "session", "client:main").Info("tool grep ok")

	view := BuildTurnView(w, ring, "run-xyz")
	if !view.Found {
		t.Fatal("Found=false want true (agentlog had events)")
	}
	if view.Session != "client:main" {
		t.Errorf("Session=%q want client:main", view.Session)
	}
	if view.Start == nil || view.Start.Model != "opus" {
		t.Errorf("Start not assembled: %+v", view.Start)
	}
	if view.End == nil || view.End.StopReason != "end_turn" {
		t.Errorf("End not assembled: %+v", view.End)
	}
	if len(view.Tools) != 1 || view.Tools[0].Name != "grep" {
		t.Errorf("Tools=%+v want one grep", view.Tools)
	}
	if len(view.Logs) != 1 {
		t.Errorf("Logs=%d want 1 (joined from ring)", len(view.Logs))
	}
}

// A run whose agentlog rotated away (or was never recorded) can still be
// inspected from the captured logs alone — Found is false but the session is
// recovered from the log lines and the logs are returned.
func TestBuildTurnView_RingOnlyWhenAgentLogEmpty(t *testing.T) {
	ring := NewRing(10)
	lc := NewCapture(slog.NewTextHandler(io.Discard, nil), ring)
	slog.New(lc).With("runId", "orphan", "session", "client:x").Warn("late log")

	view := BuildTurnView(nil, ring, "orphan") // nil agentlog
	if view.Found {
		t.Error("Found=true want false (no agentlog events)")
	}
	if view.Session != "client:x" {
		t.Errorf("Session=%q want client:x (recovered from ring)", view.Session)
	}
	if len(view.Logs) != 1 {
		t.Errorf("Logs=%d want 1", len(view.Logs))
	}
}

func TestBuildTurnView_EmptyEverything(t *testing.T) {
	view := BuildTurnView(nil, nil, "nothing")
	if view.Found || view.RunID != "nothing" || len(view.Logs) != 0 {
		t.Errorf("empty view unexpected: %+v", view)
	}
}
