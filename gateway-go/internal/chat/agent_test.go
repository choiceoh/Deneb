package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// sseResponse builds an SSE response string for a simple text completion.
func sseResponse(text, stopReason string) string {
	var b strings.Builder
	b.WriteString(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"test","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

`)
	b.WriteString(fmt.Sprintf(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"%s"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

`, text))
	b.WriteString(fmt.Sprintf(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"%s"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`, stopReason))
	return b.String()
}

// sseToolResponse builds an SSE response that contains a tool_use block.
func sseToolResponse(toolID, toolName, toolInput string) string {
	var b strings.Builder
	b.WriteString(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"test","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"` + toolID + `","name":"` + toolName + `"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"` + toolInput + `"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}

`)
	return b.String()
}

func TestRunAgent_SimpleTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("Hello world", "end_turn"))
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key")
	cfg := AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
	}

	var deltas []string
	result, err := RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "hi")},
		client, nil,
		func(text string) { deltas = append(deltas, text) },
		nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.Text != "Hello world" {
		t.Errorf("Text = %q, want %q", result.Text, "Hello world")
	}
	if result.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	if result.Turns != 1 {
		t.Errorf("Turns = %d, want 1", result.Turns)
	}
	if len(deltas) != 1 || deltas[0] != "Hello world" {
		t.Errorf("deltas = %v", deltas)
	}
}

func TestRunAgent_ToolCallLoop(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if callCount == 1 {
			// First call: model requests a tool.
			fmt.Fprint(w, sseToolResponse("tool_1", "echo", `{}`))
		} else {
			// Second call: model produces final text.
			fmt.Fprint(w, sseResponse("Done!", "end_turn"))
		}
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key")
	cfg := AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "echo", Description: "echoes input"}},
	}

	reg := NewToolRegistry()
	reg.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		return "echoed: " + string(input), nil
	})

	result, err := RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "use echo")},
		client, reg, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}
	if result.Text != "Done!" {
		t.Errorf("Text = %q, want %q", result.Text, "Done!")
	}
	if callCount != 2 {
		t.Errorf("LLM calls = %d, want 2", callCount)
	}
}

func TestRunAgent_MaxTurns(t *testing.T) {
	// Model always requests a tool — should stop at MaxTurns.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseToolResponse("tool_1", "loop", `{}`))
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key")
	cfg := AgentConfig{
		MaxTurns: 3,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "loop"}},
	}

	reg := NewToolRegistry()
	reg.Register("loop", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	result, err := RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "loop forever")},
		client, reg, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.StopReason != "max_turns" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "max_turns")
	}
	if result.Turns != 3 {
		t.Errorf("Turns = %d, want 3", result.Turns)
	}
}

func TestRunAgent_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))
	cfg := AgentConfig{
		MaxTurns: 5,
		Timeout:  200 * time.Millisecond,
		Model:    "test-model",
	}

	result, err := RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "hello")},
		client, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "timeout")
	}
}

func TestRunAgent_Abort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))
	cfg := AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := RunAgent(
		ctx, cfg,
		[]llm.Message{llm.NewTextMessage("user", "hello")},
		client, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "aborted")
	}
}

func TestRunAgent_ToolError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if callCount == 1 {
			fmt.Fprint(w, sseToolResponse("tool_1", "fail", `{}`))
		} else {
			fmt.Fprint(w, sseResponse("Handled error", "end_turn"))
		}
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key")
	cfg := AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "fail"}},
	}

	reg := NewToolRegistry()
	reg.Register("fail", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", fmt.Errorf("tool broken")
	})

	result, err := RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "use fail tool")},
		client, reg, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunAgent error: %v", err)
	}
	if result.Text != "Handled error" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}
}
