package tools

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	fn := ToolObserve(lc, w, nil, nil)

	// behavior on an empty log still produces a formatted summary; with no
	// vllmBases wired, no prefix-cache line appears.
	out, err := callTool(t, fn, map[string]any{"action": "behavior", "days": 7})
	if err != nil {
		t.Fatalf("behavior errored: %v", err)
	}
	if !strings.Contains(out, "behavior") {
		t.Errorf("behavior output missing header:\n%s", out)
	}
	if strings.Contains(out, "prefix cache") {
		t.Errorf("prefix cache line should be absent without vllmBases:\n%s", out)
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

// behavior renders the vLLM prefix-cache line when a metrics endpoint is
// reachable, and stays silent (no error, no line) when the server is down —
// the graceful-degradation contract.
func TestToolObserve_BehaviorVllmPrefixCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, `# TYPE vllm:prefix_cache_queries_total counter`)
		fmt.Fprintln(w, `vllm:prefix_cache_queries_total{model_name="deepseek-v4-flash"} 1000`)
		fmt.Fprintln(w, `vllm:prefix_cache_hits_total{model_name="deepseek-v4-flash"} 820`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	w := agentlog.NewWriter(dir)
	fn := ToolObserve(nil, w, nil, func() []string { return []string{srv.URL + "/v1"} })
	out, err := callTool(t, fn, map[string]any{"action": "behavior"})
	if err != nil {
		t.Fatalf("behavior errored: %v", err)
	}
	if !strings.Contains(out, "prefix cache (deepseek-v4-flash, since engine boot): 820/1000 (82.0%)") {
		t.Errorf("behavior output missing prefix-cache line:\n%s", out)
	}

	// Dead endpoint → line silently absent, behavior still succeeds.
	srv.Close()
	out, err = callTool(t, fn, map[string]any{"action": "behavior"})
	if err != nil {
		t.Fatalf("behavior errored with dead metrics endpoint: %v", err)
	}
	if strings.Contains(out, "prefix cache") {
		t.Errorf("prefix cache line should be absent when the endpoint is down:\n%s", out)
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

	fn := ToolObserve(nil, w, nil, nil) // nil capture: turn still works off the agent log
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
