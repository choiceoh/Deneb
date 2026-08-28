package toolport

import (
	"encoding/json"
	"testing"
)

func traceMsg(t *testing.T, id, role string, blocks ...map[string]any) ChatMessage {
	t.Helper()
	content, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	return ChatMessage{ID: id, Role: role, Content: content}
}

func TestCollectToolTracesPairsCallsWithResultsInOrder(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		traceMsg(
			t, "a1", "assistant",
			map[string]any{"type": "text", "text": "확인해볼게요"},
			map[string]any{"type": "tool_use", "id": "tu-1", "name": "exec", "input": map[string]any{"command": "ls -la"}},
			map[string]any{"type": "tool_use", "id": "tu-2", "name": "wiki", "input": map[string]any{"query": "아르고"}},
		),
		traceMsg(
			t, "u1", "user",
			map[string]any{"type": "tool_result", "tool_use_id": "tu-2", "content": "문서 A\n문서 B"},
			map[string]any{"type": "tool_result", "tool_use_id": "tu-1", "content": []map[string]any{{"type": "text", "text": "total 60\nfile.ts"}}, "is_error": false},
		),
	}

	traces := CollectToolTraces(msgs)
	items, ok := traces["a1"]
	if !ok || len(items) != 2 {
		t.Fatalf("traces[a1] = %#v, want 2 items", traces)
	}
	// tool_use order survives even though results arrived swapped.
	if items[0].Tool != "exec" || items[1].Tool != "wiki" {
		t.Fatalf("order = %s, %s; want exec, wiki", items[0].Tool, items[1].Tool)
	}
	if items[0].Detail != "ls -la" {
		t.Errorf("exec detail = %q (live hint parity)", items[0].Detail)
	}
	if items[0].Summary != "total 60 · 2줄" {
		t.Errorf("exec summary = %q", items[0].Summary)
	}
	if items[0].Preview != "total 60\nfile.ts" {
		t.Errorf("exec preview = %q", items[0].Preview)
	}
	if items[1].Summary != "문서 A · 2줄" {
		t.Errorf("wiki summary = %q", items[1].Summary)
	}
}

func TestCollectToolTracesMarksErrorsAndDropsUnfinishedCalls(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		traceMsg(
			t, "a1", "assistant",
			map[string]any{"type": "tool_use", "id": "tu-ok", "name": "exec", "input": map[string]any{"command": "run"}},
			map[string]any{"type": "tool_use", "id": "tu-orphan", "name": "web", "input": map[string]any{}},
		),
		traceMsg(
			t, "u1", "user",
			map[string]any{"type": "tool_result", "tool_use_id": "tu-ok", "content": "exit code 2", "is_error": true},
		),
	}

	traces := CollectToolTraces(msgs)
	items := traces["a1"]
	if len(items) != 1 {
		t.Fatalf("items = %#v, want the orphan dropped", items)
	}
	if !items[0].IsError || items[0].Summary != "exit code 2" {
		t.Errorf("error item = %#v", items[0])
	}
}

func TestCollectToolTracesIgnoresPlainMessagesAndMissingIDs(t *testing.T) {
	t.Parallel()
	plain := ChatMessage{ID: "p1", Role: "assistant", Content: json.RawMessage(`"그냥 텍스트"`)}
	noID := traceMsg(
		t, "", "assistant",
		map[string]any{"type": "tool_use", "id": "tu-1", "name": "exec", "input": map[string]any{}},
	)
	if traces := CollectToolTraces([]ChatMessage{plain, noID}); len(traces) != 0 {
		t.Fatalf("traces = %#v, want empty", traces)
	}
}
