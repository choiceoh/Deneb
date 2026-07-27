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
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
)

type toolExecFunc func(ctx context.Context, name string, input json.RawMessage) (string, error)

func (f toolExecFunc) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	return f(ctx, name, input)
}

func vetAll(string) bool { return true }

func TestSegmentToolCalls_SplitsAtEachVetBoundary(t *testing.T) {
	call := func(name, input string) llm.ContentBlock {
		return llm.ContentBlock{Type: "tool_use", Name: name, Input: llm.FlexibleFromRaw([]byte(input))}
	}
	vetWeb := func(name string) bool { return name == "web" }
	seg := func(start, end int, parallel bool) toolCallSegment {
		return toolCallSegment{start: start, end: end, parallel: parallel}
	}

	cases := []struct {
		name  string
		cfg   AgentConfig
		calls []llm.ContentBlock
		want  []toolCallSegment
	}{
		{
			"nil vet stays sequential",
			AgentConfig{},
			[]llm.ContentBlock{call("web", `{}`), call("web", `{}`)},
			[]toolCallSegment{seg(0, 1, false), seg(1, 2, false)},
		},
		{
			"single call is one singleton",
			AgentConfig{ParallelSafeTool: vetAll},
			[]llm.ContentBlock{call("web", `{}`)},
			[]toolCallSegment{seg(0, 1, false)},
		},
		{
			"$ref anywhere keeps the whole turn sequential",
			AgentConfig{ParallelSafeTool: vetAll},
			[]llm.ContentBlock{call("web", `{}`), call("web", `{"$ref":"tu_1"}`), call("web", `{}`)},
			[]toolCallSegment{seg(0, 1, false), seg(1, 2, false), seg(2, 3, false)},
		},
		{
			"all vetted is one parallel segment",
			AgentConfig{ParallelSafeTool: vetWeb},
			[]llm.ContentBlock{call("web", `{"u":1}`), call("web", `{"u":2}`)},
			[]toolCallSegment{seg(0, 2, true)},
		},
		{
			"unsafe call is a barrier, safe neighbors still overlap",
			AgentConfig{ParallelSafeTool: vetWeb},
			[]llm.ContentBlock{call("web", `{"u":1}`), call("web", `{"u":2}`), call("edit", `{}`), call("web", `{"u":3}`), call("web", `{"u":4}`)},
			[]toolCallSegment{seg(0, 2, true), seg(2, 3, false), seg(3, 5, true)},
		},
		{
			"lone safe call between barriers stays a singleton",
			AgentConfig{ParallelSafeTool: vetWeb},
			[]llm.ContentBlock{call("edit", `{}`), call("web", `{}`), call("edit", `{}`)},
			[]toolCallSegment{seg(0, 1, false), seg(1, 2, false), seg(2, 3, false)},
		},
	}
	for _, tc := range cases {
		got := segmentToolCalls(tc.cfg, tc.calls)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: segment[%d] = %v, want %v", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}

// TestExecuteToolsParallelTracked_OverlapsButPreservesCallOrder: three 120ms calls must
// finish in well under serial time (360ms), and the result blocks, result-hook
// emissions, and ToolUseIDs must all follow the CALL order even though the
// executions complete in a different order.
func TestExecuteToolsParallelTracked_OverlapsButPreservesCallOrder(t *testing.T) {
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
		calls[i] = llm.ContentBlock{Type: "tool_use", ID: id, Name: "web", Input: llm.FlexibleFromRaw([]byte(`{"id":"` + id + `"}`))}
	}

	start := time.Now()
	results := executeToolsParallelTracked(context.Background(), calls, exec, hooks, "", 0, slog.Default(), nil, nil).results
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

// TestToolResultMetadata_PresentOrMissingAcrossPaths: a tool that writes to the
// per-call toolmeta collector gets its values attached to the result block's
// Metadata — on both the sequential and the parallel path — and calls that set
// nothing keep the field absent.
func TestToolResultMetadata_PresentOrMissingAcrossPaths(t *testing.T) {
	exec := toolExecFunc(func(ctx context.Context, _ string, input json.RawMessage) (string, error) {
		if strings.Contains(string(input), "meta") {
			toolmeta.Set(ctx, "activatedTools", []string{"graphify"})
		}
		return "done", nil
	})
	calls := []llm.ContentBlock{
		{Type: "tool_use", ID: "a", Name: "web", Input: llm.FlexibleFromRaw([]byte(`{"meta":1}`))},
		{Type: "tool_use", ID: "b", Name: "web", Input: llm.FlexibleFromRaw([]byte(`{}`))},
	}

	check := func(t *testing.T, results []llm.ContentBlock) {
		t.Helper()
		var tools []string
		if !toolmeta.Get(results[0].Metadata.Bytes(), "activatedTools", &tools) || len(tools) != 1 {
			t.Fatalf("metadata not attached: %s", results[0].Metadata)
		}
		if !results[1].Metadata.IsZero() {
			t.Fatalf("call that set nothing must keep Metadata absent, got %s", results[1].Metadata)
		}
	}

	t.Run("parallel", func(t *testing.T) {
		check(t, executeToolsParallelTracked(context.Background(), calls, exec, StreamHooks{}, "", 0, slog.Default(), nil, nil).results)
	})
	t.Run("sequential", func(t *testing.T) {
		results := make([]llm.ContentBlock, len(calls))
		for i, tc := range calls {
			results[i] = executeOneToolTracked(context.Background(), tc, exec, StreamHooks{}, "", 0, slog.Default(), nil, nil).block
		}
		check(t, results)
	})
}

// TestExecuteToolsParallelTracked_ErrorIsolation: one failing call yields an IsError
// block at its own index without disturbing its siblings.
func TestExecuteToolsParallelTracked_ErrorIsolation(t *testing.T) {
	exec := toolExecFunc(func(_ context.Context, _ string, input json.RawMessage) (string, error) {
		if strings.Contains(string(input), "boom") {
			return "", fmt.Errorf("kaput")
		}
		return "fine", nil
	})
	calls := []llm.ContentBlock{
		{Type: "tool_use", ID: "a", Name: "web", Input: llm.FlexibleFromRaw([]byte(`{}`))},
		{Type: "tool_use", ID: "b", Name: "web", Input: llm.FlexibleFromRaw([]byte(`{"boom":1}`))},
	}
	results := executeToolsParallelTracked(context.Background(), calls, exec, StreamHooks{}, "", 0, slog.Default(), nil, nil).results
	if results[0].IsError || results[0].Content != "fine" {
		t.Errorf("healthy sibling disturbed: %+v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Content, "kaput") {
		t.Errorf("failing call must carry its error: %+v", results[1])
	}
}
