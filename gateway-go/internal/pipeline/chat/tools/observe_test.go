package tools

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
)

func TestToolObserve_RoutesAndValidates(t *testing.T) {
	dir := t.TempDir()
	w := agentlog.NewWriter(dir)
	ring := observe.NewRing(10)
	lc := observe.NewCapture(slog.NewTextHandler(io.Discard, nil), ring)
	fn := ToolObserve(lc, w)

	// behavior on an empty log still produces a formatted summary.
	out, err := callTool(t, fn, map[string]any{"action": "behavior", "days": 7})
	if err != nil {
		t.Fatalf("behavior errored: %v", err)
	}
	if !strings.Contains(out, "behavior") {
		t.Errorf("behavior output missing header:\n%s", out)
	}

	// logs on an empty ring formats cleanly (no crash).
	if _, err := callTool(t, fn, map[string]any{"action": "logs"}); err != nil {
		t.Fatalf("logs errored: %v", err)
	}

	// turn without runId is a user error.
	if _, err := callTool(t, fn, map[string]any{"action": "turn"}); err == nil {
		t.Error("turn without runId should error")
	}

	// unknown action is a user error.
	if _, err := callTool(t, fn, map[string]any{"action": "bogus"}); err == nil {
		t.Error("unknown action should error")
	}
}

// A run recorded in the agent log surfaces through observe turn, including its
// tool list — the join the self-observation tool exists to provide.
func TestToolObserve_TurnJoinsAgentLog(t *testing.T) {
	dir := t.TempDir()
	w := agentlog.NewWriter(dir)
	rl := agentlog.NewRunLogger(w, "client:main", "run-9")
	rl.LogTurnTool(agentlog.TurnToolData{Turn: 1, Name: "grep", DurationMs: 7})
	rl.LogEnd(agentlog.RunEndData{StopReason: "end_turn", Turns: 1, OutputTokens: 40})

	fn := ToolObserve(nil, w) // nil capture: turn still works off the agent log
	out, err := callTool(t, fn, map[string]any{"action": "turn", "runId": "run-9"})
	if err != nil {
		t.Fatalf("turn errored: %v", err)
	}
	for _, want := range []string{"run-9", "end_turn", "grep"} {
		if !strings.Contains(out, want) {
			t.Errorf("turn output missing %q:\n%s", want, out)
		}
	}
}
