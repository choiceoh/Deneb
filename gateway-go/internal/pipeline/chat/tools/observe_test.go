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
	rl.LogTurnTool(agentlog.TurnToolData{
		Turn:       1,
		Name:       "grep",
		ToolUseID:  "tool-1",
		DurationMs: 7,
		InputBytes: 17,
		InputHash:  "input-hash-for-test",
		OutputLen:  2,
		OutputHash: "output-hash-for-test",
		Targets:    []string{"foo.go"},
		FileEffects: []agentlog.ToolFileEffect{{
			Path:         "foo.go",
			ExistsBefore: true,
			ExistsAfter:  true,
			Changed:      true,
			AddedLines:   2,
			RemovedLines: 1,
		}},
	})
	rl.LogEnd(agentlog.RunEndData{StopReason: "end_turn", Turns: 1, OutputTokens: 40})

	fn := ToolObserve(nil, w, nil, nil) // nil capture: turn still works off the agent log
	out, err := callTool(t, fn, map[string]any{"action": "turn", "runId": "run-9"})
	if err != nil {
		t.Fatalf("turn errored: %v", err)
	}
	for _, want := range []string{"run-9", "end_turn", "grep", "id=tool-1", "in#=input-hash-f", "out#=output-hash-", "target=foo.go", "file=foo.go:changed +2/-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("turn output missing %q:\n%s", want, out)
		}
	}
}

func TestToolObserve_Provenance(t *testing.T) {
	dir := t.TempDir()
	w := agentlog.NewWriter(dir)
	rl := agentlog.NewRunLogger(w, "client:main", "run-prov")
	rl.LogTurnTool(agentlog.TurnToolData{
		Turn:       1,
		Name:       "edit",
		ToolUseID:  "tool-prov",
		InputHash:  "input-hash-for-provenance-test",
		OutputHash: "output-hash-for-provenance-test",
		Targets:    []string{"src/foo.go"},
		FileEffects: []agentlog.ToolFileEffect{{
			Path:        "src/foo.go",
			ExistsAfter: true,
			Changed:     true,
			AddedLines:  3,
		}},
	})

	fn := ToolObserve(nil, w, nil, nil)
	out, err := callTool(t, fn, map[string]any{
		"action": "provenance",
		"target": "foo.go",
	})
	if err != nil {
		t.Fatalf("provenance errored: %v", err)
	}
	for _, want := range []string{"tool provenance", "run-prov", "edit", "id=tool-prov", "target=src/foo.go", "in#=input-hash-f", "effect=src/foo.go:created +3/-0"} {
		if !strings.Contains(out, want) {
			t.Errorf("provenance output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatTurnEffort(t *testing.T) {
	// No effort signal (router inactive) → empty.
	if got := formatTurnEffort([]agentlog.TurnLLMData{{Turn: 1}, {Turn: 2}}); got != "" {
		t.Fatalf("inactive router must render nothing, got %q", got)
	}
	// A routed run: turn 1 off (no obs), turn 2 off with obs, turn 3 reverted on.
	turns := []agentlog.TurnLLMData{
		{Turn: 1, ThinkingOff: true},
		{Turn: 2, ThinkingOff: true, ObsRunes: 1500},
		{Turn: 3, ObsRunes: 8200},
	}
	got := formatTurnEffort(turns)
	for _, want := range []string{"effort:", "t1:off/obs=0", "t2:off/obs=1500", "t3:on/obs=8200"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}
