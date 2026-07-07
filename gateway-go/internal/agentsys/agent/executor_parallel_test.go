package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type toolExecFunc func(ctx context.Context, name string, input json.RawMessage) (string, error)

func (f toolExecFunc) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	return f(ctx, name, input)
}

func vetAll(string) bool { return true }

func TestParallelSafeTurn(t *testing.T) {
	call := func(name, input string) llm.ContentBlock {
		return llm.ContentBlock{Type: "tool_use", Name: name, Input: json.RawMessage(input)}
	}
	vetWeb := func(name string) bool { return name == "web" }

	cases := []struct {
		name  string
		cfg   AgentConfig
		calls []llm.ContentBlock
		want  bool
	}{
		{"nil vet stays sequential", AgentConfig{}, []llm.ContentBlock{call("web", `{}`), call("web", `{}`)}, false},
		{"single call stays sequential", AgentConfig{ParallelSafeTool: vetAll}, []llm.ContentBlock{call("web", `{}`)}, false},
		{"unvetted tool stays sequential", AgentConfig{ParallelSafeTool: vetWeb}, []llm.ContentBlock{call("web", `{}`), call("edit", `{}`)}, false},
		{"$ref piping stays sequential", AgentConfig{ParallelSafeTool: vetAll}, []llm.ContentBlock{call("web", `{}`), call("web", `{"$ref":"tu_1"}`)}, false},
		{"all vetted goes parallel", AgentConfig{ParallelSafeTool: vetWeb}, []llm.ContentBlock{call("web", `{"u":1}`), call("web", `{"u":2}`)}, true},
	}
	for _, tc := range cases {
		if got := parallelSafeTurn(tc.cfg, tc.calls); got != tc.want {
			t.Errorf("%s: parallelSafeTurn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestExecuteToolsParallel_OverlapsAndKeepsOrder: three 120ms calls must
// finish in well under serial time (360ms), and the result blocks, result-hook
// emissions, and ToolUseIDs must all follow the CALL order even though the
// executions complete in a different order.
func TestExecuteToolsParallel_OverlapsAndKeepsOrder(t *testing.T) {
	sleeps := map[string]time.Duration{"tu_0": 120 * time.Millisecond, "tu_1": 60 * time.Millisecond, "tu_2": 10 * time.Millisecond}
	exec := toolExecFunc(func(ctx context.Context, name string, input json.RawMessage) (string, error) {
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(input, &p)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(sleeps[p.ID]):
		}
		return "out-" + p.ID, nil
	})

	var mu sync.Mutex
	var resultOrder []string
	hooks := StreamHooks{OnToolResult: func(name, toolUseID, content string, isError bool) {
		mu.Lock()
		resultOrder = append(resultOrder, toolUseID)
		mu.Unlock()
	}}

	calls := make([]llm.ContentBlock, 3)
	for i := range calls {
		id := fmt.Sprintf("tu_%d", i)
		calls[i] = llm.ContentBlock{Type: "tool_use", ID: id, Name: "web", Input: json.RawMessage(`{"id":"` + id + `"}`)}
	}

	start := time.Now()
	results := executeToolsParallel(context.Background(), calls, exec, hooks, "", 0, slog.Default(), nil, nil)
	elapsed := time.Since(start)

	if elapsed >= 300*time.Millisecond {
		t.Errorf("parallel execution took %v — looks serial (3×120/60/10ms)", elapsed)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for i, r := range results {
		wantID := fmt.Sprintf("tu_%d", i)
		if r.ToolUseID != wantID || r.Content != "out-"+wantID || r.IsError {
			t.Errorf("result[%d] = {id:%s content:%q err:%v}, want ordered out-%s", i, r.ToolUseID, r.Content, r.IsError, wantID)
		}
	}
	// Hook emissions replay in call order even though tu_2 finished first.
	if strings.Join(resultOrder, ",") != "tu_0,tu_1,tu_2" {
		t.Errorf("OnToolResult order = %v, want call order", resultOrder)
	}
}

// TestExecuteToolsParallel_ErrorIsolation: one failing call yields an IsError
// block at its own index without disturbing its siblings.
func TestExecuteToolsParallel_ErrorIsolation(t *testing.T) {
	exec := toolExecFunc(func(_ context.Context, _ string, input json.RawMessage) (string, error) {
		if strings.Contains(string(input), "boom") {
			return "", fmt.Errorf("kaput")
		}
		return "fine", nil
	})
	calls := []llm.ContentBlock{
		{Type: "tool_use", ID: "a", Name: "web", Input: json.RawMessage(`{}`)},
		{Type: "tool_use", ID: "b", Name: "web", Input: json.RawMessage(`{"boom":1}`)},
	}
	results := executeToolsParallel(context.Background(), calls, exec, StreamHooks{}, "", 0, slog.Default(), nil, nil)
	if results[0].IsError || results[0].Content != "fine" {
		t.Errorf("healthy sibling disturbed: %+v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Content, "kaput") {
		t.Errorf("failing call must carry its error: %+v", results[1])
	}
}
