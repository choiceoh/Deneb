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

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// sseResponse builds an OpenAI-compatible SSE response for a simple text completion.
func sseResponse(text, stopReason string) string {
	// Map Anthropic stop reasons to OpenAI finish_reasons.
	finishReason := stopReason
	if finishReason == "end_turn" {
		finishReason = "stop"
	}

	var b strings.Builder
	// Delta chunk with content.
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"%s\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n", text)
	// Final chunk with finish_reason.
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"%s\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n", finishReason)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// sseToolResponse builds an OpenAI-compatible SSE response with a tool call.
func sseToolResponse(toolID, toolName, toolInput string) string {
	var b strings.Builder
	// First chunk: tool call declaration.
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"%s\",\"type\":\"function\",\"function\":{\"name\":\"%s\",\"arguments\":\"\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n", toolID, toolName)
	// Second chunk: tool call arguments.
	fmt.Fprintf(&b, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"%s\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n", toolInput)
	// Final chunk: finish with tool_calls reason.
	b.WriteString("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":10}}\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// newTestLLMClient creates an httptest server and LLM client for agent tests.
func newTestLLMClient(t *testing.T, handler http.HandlerFunc, opts ...llm.ClientOption) *llm.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return llm.NewClient(server.URL, "test-key", opts...)
}

func TestRunAgent_SimpleTextResponse(t *testing.T) {
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("Hello world", "end_turn"))
	})
	cfg := agent.AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
	}

	var deltas []string
	result, err := agent.RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "hi")},
		client, nil,
		agent.StreamHooks{OnTextDelta: func(text string) { deltas = append(deltas, text) }},
		nil, nil,
	)
	testutil.NoError(t, err)
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
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	})
	cfg := agent.AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "echo", Description: "echoes input"}},
	}

	reg := NewToolRegistry()
	reg.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		return "echoed: " + string(input), nil
	})

	result, err := agent.RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "use echo")},
		client, reg, agent.StreamHooks{}, nil, nil,
	)
	testutil.NoError(t, err)
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
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseToolResponse("tool_1", "loop", `{}`))
	})
	cfg := agent.AgentConfig{
		MaxTurns: 3,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "loop"}},
	}

	reg := NewToolRegistry()
	reg.Register("loop", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	result, err := agent.RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "loop forever")},
		client, reg, agent.StreamHooks{}, nil, nil,
	)
	testutil.NoError(t, err)
	if result.StopReason != "max_turns" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "max_turns")
	}
	if result.Turns != 3 {
		t.Errorf("Turns = %d, want 3", result.Turns)
	}
}

func TestRunAgent_Timeout(t *testing.T) {
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}, llm.WithRetry(0, 0, 0))
	cfg := agent.AgentConfig{
		MaxTurns: 5,
		Timeout:  200 * time.Millisecond,
		Model:    "test-model",
	}

	result, err := agent.RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "hello")},
		client, nil, agent.StreamHooks{}, nil, nil,
	)
	testutil.NoError(t, err)
	if result.StopReason != "timeout" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "timeout")
	}
}

func TestRunAgent_Abort(t *testing.T) {
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}, llm.WithRetry(0, 0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	cfg := agent.AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := agent.RunAgent(
		ctx, cfg,
		[]llm.Message{llm.NewTextMessage("user", "hello")},
		client, nil, agent.StreamHooks{}, nil, nil,
	)
	testutil.NoError(t, err)
	if result.StopReason != "aborted" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "aborted")
	}
}

func TestRunAgent_ToolError(t *testing.T) {
	callCount := 0
	client := newTestLLMClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if callCount == 1 {
			fmt.Fprint(w, sseToolResponse("tool_1", "fail", `{}`))
		} else {
			fmt.Fprint(w, sseResponse("Handled error", "end_turn"))
		}
	})
	cfg := agent.AgentConfig{
		MaxTurns: 5,
		Timeout:  10 * time.Second,
		Model:    "test-model",
		Tools:    []llm.Tool{{Name: "fail"}},
	}

	reg := NewToolRegistry()
	reg.Register("fail", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", fmt.Errorf("tool broken")
	})

	result, err := agent.RunAgent(
		context.Background(), cfg,
		[]llm.Message{llm.NewTextMessage("user", "use fail tool")},
		client, reg, agent.StreamHooks{}, nil, nil,
	)
	testutil.NoError(t, err)
	if result.Text != "Handled error" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Turns != 2 {
		t.Errorf("Turns = %d, want 2", result.Turns)
	}
}
