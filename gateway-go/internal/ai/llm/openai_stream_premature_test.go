// openai_stream_premature_test.go — premature stream-end behavior of the
// OpenAI SSE translator. Two regimes, split by who ended the stream:
//
//   - [DONE] without a finish_reason chunk: the SERVER ended the stream —
//     treat as a clean stop and rescue buffered tool calls (valid-JSON args
//     only) so a dropped finish chunk doesn't discard the turn's work.
//   - bare EOF with neither finish_reason nor [DONE]: the TRANSPORT died
//     (close-delimited connection cut, empty 200 body) — surface a terminal
//     error event. Synthesizing message_stop here delivered empty/truncated
//     turns as successes (PR #2268 review, reproduced live by killing an
//     HTTP/1.0 broker mid-response).
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

// requireTerminalError asserts the stream's last event is an error event whose
// payload mentions wantSubstr, and that no message_stop was synthesized — the
// invariant that keeps a cut stream from masquerading as a successful turn.
func requireTerminalError(t *testing.T, got []StreamEvent, wantSubstr string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatal("no events received")
	}
	last := got[len(got)-1]
	if last.Type != "error" {
		t.Fatalf("terminal event = %q, want error (events: %v)", last.Type, eventTypes(got))
	}
	if !strings.Contains(string(last.Payload), wantSubstr) {
		t.Errorf("error payload = %s, want mention of %q", last.Payload, wantSubstr)
	}
	for _, ev := range got {
		if ev.Type == "message_stop" {
			t.Error("message_stop must not be synthesized on a cut stream")
		}
	}
}

func eventTypes(events []StreamEvent) []string {
	types := make([]string, len(events))
	for i, ev := range events {
		types[i] = ev.Type
	}
	return types
}

// TestStreamChat_EOFWithoutDone_SurfacesErrorNotSuccess pins the EOF regime:
// a clean connection EOF with neither finish_reason nor [DONE] is a transport
// failure, not a stop. The buffered tool call must be DROPPED (the flush
// rescue is reserved for an explicit [DONE]) and the terminal event must be
// an error so the executor retries instead of committing the turn.
func TestStreamChat_EOFWithoutDone_SurfacesErrorNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", toolCallChunk(0, "call_eof", "health", `{}`))
		flusher.Flush()
		// Handler returns — connection closes with no finish chunk, no [DONE].
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	requireTerminalError(t, got, "finish_reason")
	for _, ev := range got {
		if ev.Type != "content_block_start" {
			continue
		}
		var cbs ContentBlockStart
		testutil.NoError(t, json.Unmarshal(ev.Payload, &cbs))
		if cbs.ContentBlock.Type == "tool_use" {
			t.Error("buffered tool call must be dropped on mid-stream EOF, not flushed")
		}
	}
}

// TestStreamChat_MidStreamEOF_AfterTextDeltas_SurfacesError covers the
// user-facing shape of the bug: text was streaming, the connection died, and
// the truncated reply must NOT be committed as a successful turn.
func TestStreamChat_MidStreamEOF_AfterTextDeltas_SurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-cut", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": "truncated rep"}},
			},
		}))
		flusher.Flush()
		// Connection cut before any finish_reason chunk.
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	var sawTextDelta bool
	for _, ev := range got {
		if ev.Type == "content_block_delta" {
			sawTextDelta = true
		}
	}
	if !sawTextDelta {
		t.Fatal("text delta missing — fixture should cut mid-stream, not pre-stream")
	}
	requireTerminalError(t, got, "finish_reason")
}

// TestStreamChat_EmptyStreamEOF_SurfacesError: an HTTP 200 with an empty SSE
// body (the killed-broker live repro) must be an error, not an empty success.
func TestStreamChat_EmptyStreamEOF_SurfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// No body at all.
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	requireTerminalError(t, got, "0 chunks")
}

// TestStreamChat_FinishReasonWithoutDone_NormalStop guards the other
// direction: some OpenAI-compatible servers omit the [DONE] sentinel and just
// close after the finish_reason chunk. That is a server-declared clean end —
// it must keep producing a normal message_stop, never an error.
func TestStreamChat_FinishReasonWithoutDone_NormalStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-nodone", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": "complete reply"}},
			},
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
			"id": "chatcmpl-nodone", "model": "test-model",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
			},
		}))
		flusher.Flush()
		// EOF without [DONE].
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	if len(got) == 0 {
		t.Fatal("no events received")
	}
	if last := got[len(got)-1]; last.Type != "message_stop" {
		t.Errorf("terminal event = %q, want message_stop (finish_reason counts as a clean end)", last.Type)
	}
	for _, ev := range got {
		if ev.Type == "error" {
			t.Errorf("unexpected error event on a finish_reason-terminated stream: %s", ev.Payload)
		}
	}
}

// TestStreamChat_ErrorJSONBody_SurfacedNotSwallowed: a bare {"error":{...}}
// data line unmarshals into a zero-valued openAIChunk, so it used to be
// swallowed as an empty usage chunk — the provider's own error message
// vanished and the turn ended as an empty success. It must surface as a
// terminal error event carrying that message.
func TestStreamChat_ErrorJSONBody_SurfacedNotSwallowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"backend exploded\",\"type\":\"server_error\"}}\n\n")
	}))
	defer server.Close()

	got := collectStreamEvents(t, NewClient(server.URL, "test-key"))

	requireTerminalError(t, got, "backend exploded")
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
