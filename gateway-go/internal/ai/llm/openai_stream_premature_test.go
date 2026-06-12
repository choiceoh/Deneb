// openai_stream_premature_test.go — premature stream-end behavior of the
// OpenAI SSE translator: tool calls buffered for contiguous emission must not
// be silently discarded when the stream ends without a finish_reason chunk
// ([DONE] arriving early, or the connection cutting mid-stream).
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// toolCallChunk builds a streamed tool-call fragment chunk.
func toolCallChunk(idx int, id, name, argsFragment string) string {
	tc := map[string]any{
		"index":    idx,
		"function": map[string]string{"arguments": argsFragment},
	}
	if id != "" {
		tc["id"] = id
	}
	if name != "" {
		tc["function"] = map[string]string{"name": name, "arguments": argsFragment}
	}
	return mustJSON(map[string]any{
		"id":    "chatcmpl-p",
		"model": "test-model",
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{tc}}},
		},
	})
}

// collectStreamEvents drains the translated event channel into a slice.
func collectStreamEvents(t *testing.T, client *Client) []StreamEvent {
	t.Helper()
	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model:     "test-model",
		Messages:  []Message{NewTextMessage("user", "hi")},
		MaxTokens: 100,
	})
	testutil.NoError(t, err)
	var got []StreamEvent
	for ev := range events {
		got = append(got, ev)
	}
	return got
}

// TestStreamChat_PrematureDone_FlushesBufferedToolCalls verifies that a tool
// call whose arguments completed streaming is still emitted when [DONE]
// arrives without a finish_reason chunk. Before the flush, a connection cut
// right before the finish chunk silently discarded every tool call of the
// turn — the agent saw an empty assistant reply instead.
func TestStreamChat_PrematureDone_FlushesBufferedToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", toolCallChunk(0, "call_abc", "read_file", `{"path":`))
		fmt.Fprintf(w, "data: %s\n\n", toolCallChunk(0, "", "", `"/tmp/x"}`))
		// No finish_reason chunk — straight to [DONE].
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	var sawToolStart bool
	var args string
	for _, ev := range got {
		switch ev.Type {
		case "content_block_start":
			var cbs ContentBlockStart
			testutil.NoError(t, json.Unmarshal(ev.Payload, &cbs))
			if cbs.ContentBlock.Type == "tool_use" {
				sawToolStart = true
				if cbs.ContentBlock.ID != "call_abc" || cbs.ContentBlock.Name != "read_file" {
					t.Errorf("tool block = id %q name %q, want call_abc/read_file",
						cbs.ContentBlock.ID, cbs.ContentBlock.Name)
				}
			}
		case "content_block_delta":
			var cbd ContentBlockDelta
			testutil.NoError(t, json.Unmarshal(ev.Payload, &cbd))
			if cbd.Delta.Type == "input_json_delta" {
				args += cbd.Delta.PartialJSON
			}
		}
	}
	if !sawToolStart {
		t.Fatal("buffered tool call was not flushed on premature [DONE]")
	}
	if args != `{"path":"/tmp/x"}` {
		t.Errorf("flushed args = %q, want full accumulated JSON", args)
	}
	if got[len(got)-1].Type != "message_stop" {
		t.Errorf("last event = %q, want message_stop", got[len(got)-1].Type)
	}
}

// TestStreamChat_PrematureDone_DropsTruncatedToolArgs verifies the safety
// valve: a tool call whose arguments were cut mid-JSON must NOT be flushed —
// executing half-specified arguments would perform a half-specified action.
func TestStreamChat_PrematureDone_DropsTruncatedToolArgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", toolCallChunk(0, "call_cut", "write_file", `{"path":"/tmp/x","content":"trunc`))
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	for _, ev := range got {
		if ev.Type != "content_block_start" {
			continue
		}
		var cbs ContentBlockStart
		testutil.NoError(t, json.Unmarshal(ev.Payload, &cbs))
		if cbs.ContentBlock.Type == "tool_use" {
			t.Fatalf("tool call with truncated args must be dropped, got block %q", cbs.ContentBlock.Name)
		}
	}
}

// TestStreamChat_EOFWithoutDone_FlushesToolCalls verifies the flush also fires
// when the connection closes without even a [DONE] sentinel (mid-stream EOF).
func TestStreamChat_EOFWithoutDone_FlushesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", toolCallChunk(0, "call_eof", "health", `{}`))
		flusher.Flush()
		// Handler returns — connection closes with no finish chunk, no [DONE].
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	var sawTool bool
	for _, ev := range got {
		if ev.Type != "content_block_start" {
			continue
		}
		var cbs ContentBlockStart
		testutil.NoError(t, json.Unmarshal(ev.Payload, &cbs))
		if cbs.ContentBlock.Type == "tool_use" && cbs.ContentBlock.Name == "health" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatal("buffered tool call was not flushed on mid-stream EOF")
	}
}

// TestConvertMessages_ThinkingOnlyAssistantSkipped verifies that an assistant
// history message whose only content was thinking blocks (dropped when
// preserveThinking is off) is omitted entirely instead of being sent as
// {"role":"assistant","content":null}.
func TestConvertMessages_ThinkingOnlyAssistantSkipped(t *testing.T) {
	cap := &captureRequest{}
	server := httptest.NewServer(cap.handler(t))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	thinkingOnly, err := json.Marshal([]ContentBlock{{Type: "thinking", Thinking: "private chain of thought"}})
	testutil.NoError(t, err)

	events, err := client.StreamChat(context.Background(), ChatRequest{
		Model: "test-model",
		Messages: []Message{
			NewTextMessage("user", "first"),
			{Role: "assistant", Content: thinkingOnly},
			NewTextMessage("user", "second"),
		},
		MaxTokens: 100,
	})
	testutil.NoError(t, err)
	for range events { // drain
	}

	cap.mu.Lock()
	body := string(cap.body)
	cap.mu.Unlock()

	if strings.Contains(body, `"assistant"`) {
		t.Errorf("thinking-only assistant message must be skipped, body: %s", body)
	}
	if strings.Contains(body, "private chain of thought") {
		t.Errorf("thinking text must not leak to the wire, body: %s", body)
	}
	for _, want := range []string{`"first"`, `"second"`} {
		if !strings.Contains(body, want) {
			t.Errorf("user message %s missing from body: %s", want, body)
		}
	}
}
