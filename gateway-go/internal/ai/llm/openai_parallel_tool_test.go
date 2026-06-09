package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// assembleSingleBlock mirrors executor.consumeStreamInto's assembly: it tracks a
// SINGLE active block, overwrites it on content_block_start WITHOUT finalizing
// the previous one (so an un-stopped block is lost), discards a delta whose index
// doesn't match the active block, and finalizes on content_block_stop. The
// translator must therefore emit each block contiguously (start → deltas → stop)
// for the real consumer to assemble it — this helper is the contract under test.
func assembleSingleBlock(events <-chan StreamEvent) []ContentBlock {
	var out []ContentBlock
	var cur *ContentBlock
	var curJSON []byte
	curIndex := -1
	for ev := range events {
		switch ev.Type {
		case "content_block_start":
			var cbs ContentBlockStart
			if json.Unmarshal(ev.Payload, &cbs) == nil {
				curIndex = cbs.Index
				b := cbs.ContentBlock
				cur = &b
				curJSON = nil
			}
		case "content_block_delta":
			var cbd ContentBlockDelta
			if json.Unmarshal(ev.Payload, &cbd) == nil && cur != nil && cbd.Index == curIndex {
				switch cbd.Delta.Type {
				case "input_json_delta":
					curJSON = append(curJSON, cbd.Delta.PartialJSON...)
				case "text_delta":
					cur.Text += cbd.Delta.Text
				case "thinking_delta":
					// thinking text is carried on Delta.Text (see emitDelta).
					cur.Thinking += cbd.Delta.Text
				}
			}
		case "content_block_stop":
			if cur != nil {
				if cur.Type == "tool_use" && len(curJSON) > 0 {
					cur.Input = json.RawMessage(curJSON)
				}
				out = append(out, *cur)
				cur = nil
			}
		}
	}
	return out
}

// Two parallel tool calls whose argument fragments arrive INTERLEAVED across
// indices must both survive assembly with their full arguments. The translator
// emits them as contiguous blocks at finish; a single-active-block consumer then
// assembles each correctly. (TestStreamChat_MultipleToolCalls only checks the
// raw event stream with a per-index map, so it can't catch the assembly bug.)
func TestStreamChat_ParallelToolCalls_AssembleViaSingleBlock(t *testing.T) {
	chunks := []map[string]any{
		// Tool 0 (read) starts.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 0, "id": "call_1", "type": "function",
				"function": map[string]string{"name": "read", "arguments": `{"pa`}}}}}}},
		// Tool 1 (grep) starts — before tool 0's args finish.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 1, "id": "call_2", "type": "function",
				"function": map[string]string{"name": "grep", "arguments": `{"pat`}}}}}}},
		// More args for tool 0 (interleaved).
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 0,
				"function": map[string]string{"arguments": `th":"f.go"}`}}}}}}},
		// More args for tool 1.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0, "delta": map[string]any{
			"tool_calls": []map[string]any{{"index": 1,
				"function": map[string]string{"arguments": `tern":"foo"}`}}}}}}},
		// Finish.
		{"id": "c", "model": "m", "choices": []map[string]any{{"index": 0,
			"delta": map[string]string{}, "finish_reason": "tool_calls"}}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(c))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "read and search")},
		MaxTokens: 100,
	})
	testutil.NoError(t, err)

	got := map[string]string{}
	for _, b := range assembleSingleBlock(events) {
		if b.Type == "tool_use" {
			got[b.Name] = string(b.Input)
		}
	}

	if len(got) != 2 {
		t.Fatalf("got %d tool calls, want 2 (a parallel call was dropped): %+v", len(got), got)
	}
	if got["read"] != `{"path":"f.go"}` {
		t.Errorf("read args = %q, want %q", got["read"], `{"path":"f.go"}`)
	}
	if got["grep"] != `{"pattern":"foo"}` {
		t.Errorf("grep args = %q, want %q", got["grep"], `{"pattern":"foo"}`)
	}
}
