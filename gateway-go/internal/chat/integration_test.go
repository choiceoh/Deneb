package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// --- End-to-end chat execution integration tests ---
//
// These tests exercise the full async pipeline: Send() -> runAgentAsync() ->
// executeAgentRun() -> RunAgent() -> LLM mock -> transcript persistence ->
// broadcast events -> session lifecycle transitions.
//
// All external deps (LLM API, transcript store, session manager) are
// in-memory or httptest-based; no FFI or real providers needed.

// broadcastCollector captures broadcast events for assertion.
type broadcastCollector struct {
	mu     sync.Mutex
	events []broadcastEvent
}

type broadcastEvent struct {
	Event   string
	Payload any
}

func (bc *broadcastCollector) broadcast(event string, payload any) (int, []error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.events = append(bc.events, broadcastEvent{Event: event, Payload: payload})
	return 1, nil
}

func (bc *broadcastCollector) get() []broadcastEvent {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	out := make([]broadcastEvent, len(bc.events))
	copy(out, bc.events)
	return out
}

// rawBroadcastCollector captures raw broadcast events (streaming deltas, etc).
type rawBroadcastCollector struct {
	mu     sync.Mutex
	events []rawEvent
}

type rawEvent struct {
	Event string
	Data  json.RawMessage
}

func (rc *rawBroadcastCollector) broadcastRaw(event string, data []byte) int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	rc.events = append(rc.events, rawEvent{Event: event, Data: cp})
	return 1
}

func (rc *rawBroadcastCollector) get() []rawEvent {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	out := make([]rawEvent, len(rc.events))
	copy(out, rc.events)
	return out
}

// waitForSessionStatus polls until the session reaches the expected status or
// times out. Returns the final observed status.
func waitForSessionStatus(sm *session.Manager, key string, want session.RunStatus, timeout time.Duration) session.RunStatus {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := sm.Get(key)
		if s != nil && s.Status == want {
			return s.Status
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := sm.Get(key)
	if s != nil {
		return s.Status
	}
	return ""
}

// waitForGoroutineCountAtMost polls runtime.NumGoroutine until the count is at
// most max, or times out. It allows short-lived goroutines spawned by net/http
// and test helpers to settle before asserting leak-free shutdown paths.
func waitForGoroutineCountAtMost(max int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= max {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return runtime.NumGoroutine() <= max
}

// newIntegrationHandler creates a Handler wired with an httptest LLM server,
// in-memory transcript, and broadcast collectors.
func newIntegrationHandler(
	server *httptest.Server,
	transcript TranscriptStore,
) (*Handler, *session.Manager, *broadcastCollector, *rawBroadcastCollector) {
	sm := session.NewManager()
	bc := &broadcastCollector{}
	rc := &rawBroadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	h.SetBroadcastRaw(rc.broadcastRaw)
	return h, sm, bc, rc
}

// TestIntegration_SimpleTextResponse verifies the full async flow for a simple
// text response: Send -> LLM response -> transcript persisted -> session done.
func TestIntegration_SimpleTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("Integration test response", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h, sm, bc, rc := newIntegrationHandler(server, transcript)
	defer h.Close()

	// Send a message.
	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "int-test-1",
		"message":     "hello",
		"clientRunId": "run-int-1",
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Fatalf("Send failed: %v", resp.Error)
	}

	// Wait for the async run to complete (session should transition to done).
	status := waitForSessionStatus(sm, "int-test-1", session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want %q", status, session.StatusDone)
	}

	// Verify transcript has both user and assistant messages.
	msgs, total, err := transcript.Load("int-test-1", 0)
	if err != nil {
		t.Fatalf("transcript load error: %v", err)
	}
	if total < 2 {
		t.Fatalf("transcript total = %d, want >= 2", total)
	}

	// First message should be user.
	if msgs[0].Role != "user" || msgs[0].TextContent() != "hello" {
		t.Errorf("msgs[0] = {%s, %q}, want {user, hello}", msgs[0].Role, msgs[0].TextContent())
	}
	// Last message should be assistant.
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" || last.TextContent() != "Integration test response" {
		t.Errorf("last msg = {%s, %q}, want {assistant, Integration test response}",
			last.Role, last.TextContent())
	}

	// Verify broadcast events were emitted.
	events := bc.get()
	hasStarted := false
	hasCompleted := false
	for _, ev := range events {
		if ev.Event == "sessions.changed" {
			p, ok := ev.Payload.(map[string]any)
			if ok {
				if p["status"] == "running" {
					hasStarted = true
				}
				if p["status"] == "done" {
					hasCompleted = true
				}
			}
		}
	}
	if !hasStarted {
		t.Error("missing sessions.changed running event")
	}
	if !hasCompleted {
		t.Error("missing sessions.changed done event")
	}

	// Verify streaming deltas were emitted via raw broadcast.
	rawEvents := rc.get()
	hasDelta := false
	for _, ev := range rawEvents {
		if ev.Event == "chat.delta" {
			hasDelta = true
			break
		}
	}
	if !hasDelta {
		t.Error("missing chat.delta streaming event")
	}
}

// TestIntegration_ToolCallFlow tests the full async flow with a tool call:
// Send -> LLM requests tool -> tool executes -> LLM responds with final text.
func TestIntegration_ToolCallFlow(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if callCount == 1 {
			fmt.Fprint(w, sseToolResponse("tool_1", "greet", `{\"name\":\"Peter\"}`))
		} else {
			fmt.Fprint(w, sseResponse("Hello Peter!", "end_turn"))
		}
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}
	rc := &rawBroadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	tools := NewToolRegistry()
	tools.Register("greet", func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Name string `json:"name"`
		}
		json.Unmarshal(input, &p)
		return fmt.Sprintf("Greeting for %s", p.Name), nil
	})

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.Tools = tools
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	h.SetBroadcastRaw(rc.broadcastRaw)
	defer h.Close()

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "int-tool-1",
		"message":     "greet Peter",
		"clientRunId": "run-tool-1",
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Fatalf("Send failed: %v", resp.Error)
	}

	status := waitForSessionStatus(sm, "int-tool-1", session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want %q", status, session.StatusDone)
	}

	// Verify transcript contains the final assistant response.
	msgs, _, err := transcript.Load("int-tool-1", 0)
	if err != nil {
		t.Fatalf("transcript load: %v", err)
	}
	last := msgs[len(msgs)-1]
	// The persisted assistant message includes a tool activity summary prefix
	// (e.g., "Tools used: greet\n\n") followed by the actual response text.
	wantSuffix := "Hello Peter!"
	if last.Role != "assistant" || !strings.HasSuffix(last.TextContent(), wantSuffix) {
		t.Errorf("last msg = {%s, %q}, want suffix %q", last.Role, last.TextContent(), wantSuffix)
	}

	// LLM should have been called at least twice (tool call + final response).
	// Nudge continuations may add extra calls.
	if callCount < 2 {
		t.Errorf("LLM call count = %d, want >= 2", callCount)
	}
}

// TestIntegration_AbortActiveRun tests that aborting an active run transitions
// the session correctly and cancels the LLM call.
func TestIntegration_AbortActiveRun(t *testing.T) {
	baseline := runtime.NumGoroutine()

	// LLM server that blocks indefinitely until context is canceled.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Send initial event to establish connection.
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}))

	transcript := NewMemoryTranscriptStore()
	h, sm, _, _ := newIntegrationHandler(server, transcript)

	// Start a run.
	sendReq := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "int-abort-1",
		"message":     "long running task",
		"clientRunId": "run-abort-1",
	})
	resp := h.Send(context.Background(), sendReq)
	if !resp.OK {
		t.Fatalf("Send failed: %v", resp.Error)
	}

	// Wait for session to be running.
	waitForSessionStatus(sm, "int-abort-1", session.StatusRunning, 2*time.Second)

	// Abort by runId.
	abortReq := makeReq("2", "sessions.abort", map[string]any{"runId": "run-abort-1"})
	abortResp := h.SessionsAbort(context.Background(), abortReq)
	if !abortResp.OK {
		t.Fatalf("SessionsAbort failed: %v", abortResp.Error)
	}

	var abortPayload map[string]any
	json.Unmarshal(abortResp.Payload, &abortPayload)
	if abortPayload["status"] != "aborted" {
		t.Errorf("abort status = %v, want aborted", abortPayload["status"])
	}

	// Explicitly close all long-lived resources, then ensure no goroutine leak
	// remains from the cancel path (run goroutine + abort GC loop).
	h.Close()
	server.Close()

	if ok := waitForGoroutineCountAtMost(baseline+2, 2*time.Second); !ok {
		t.Fatalf(
			"possible goroutine leak after abort path: before=%d after=%d",
			baseline,
			runtime.NumGoroutine(),
		)
	}
}

// TestIntegration_MultipleMessagesHistory tests that multiple Send calls
// accumulate messages in the transcript and are visible via History.
func TestIntegration_MultipleMessagesHistory(t *testing.T) {
	msgIdx := 0
	responses := []string{"First reply", "Second reply"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		idx := msgIdx
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		msgIdx++
		fmt.Fprint(w, sseResponse(responses[idx], "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h, sm, _, _ := newIntegrationHandler(server, transcript)
	defer h.Close()

	sessionKey := "int-history-1"

	// Send first message.
	req1 := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "first message",
		"clientRunId": "run-h-1",
	})
	h.Send(context.Background(), req1)
	waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)

	// Send second message.
	req2 := makeReq("2", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "second message",
		"clientRunId": "run-h-2",
	})
	h.Send(context.Background(), req2)
	waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)

	// Verify transcript has 4 messages (2 user + 2 assistant).
	msgs, total, err := transcript.Load(sessionKey, 0)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if msgs[0].Role != "user" || msgs[0].TextContent() != "first message" {
		t.Errorf("msgs[0] = {%s, %q}", msgs[0].Role, msgs[0].TextContent())
	}
	if msgs[1].Role != "assistant" || msgs[1].TextContent() != "First reply" {
		t.Errorf("msgs[1] = {%s, %q}", msgs[1].Role, msgs[1].TextContent())
	}
	if msgs[2].Role != "user" || msgs[2].TextContent() != "second message" {
		t.Errorf("msgs[2] = {%s, %q}", msgs[2].Role, msgs[2].TextContent())
	}
	if msgs[3].Role != "assistant" || msgs[3].TextContent() != "Second reply" {
		t.Errorf("msgs[3] = {%s, %q}", msgs[3].Role, msgs[3].TextContent())
	}

	// Verify History RPC returns the messages.
	histReq := makeReq("3", "chat.history", map[string]any{"sessionKey": sessionKey})
	histResp := h.History(context.Background(), histReq)
	if !histResp.OK {
		t.Fatalf("History failed: %v", histResp.Error)
	}
	var histPayload struct {
		Messages []ChatMessage `json:"messages"`
		Total    int           `json:"total"`
	}
	json.Unmarshal(histResp.Payload, &histPayload)
	if histPayload.Total != 4 {
		t.Errorf("history total = %d, want 4", histPayload.Total)
	}
}

// TestIntegration_ReplyFunc tests that the assistant response is delivered
// back to the originating channel via ReplyFunc.
func TestIntegration_ReplyFunc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("Channel reply", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h, sm, _, _ := newIntegrationHandler(server, transcript)
	defer h.Close()

	var repliedMu sync.Mutex
	var repliedText string
	var repliedDelivery *DeliveryContext

	h.SetReplyFunc(func(_ context.Context, d *DeliveryContext, text string) error {
		repliedMu.Lock()
		defer repliedMu.Unlock()
		repliedText = text
		repliedDelivery = d
		return nil
	})

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "int-reply-1",
		"message":     "hello",
		"clientRunId": "run-reply-1",
		"delivery": map[string]any{
			"channel": "telegram",
			"to":      "user123",
		},
	})
	resp := h.Send(context.Background(), req)
	if !resp.OK {
		t.Fatalf("Send failed: %v", resp.Error)
	}

	waitForSessionStatus(sm, "int-reply-1", session.StatusDone, 5*time.Second)

	// Give a tiny bit of time for reply func to be called.
	time.Sleep(50 * time.Millisecond)

	repliedMu.Lock()
	defer repliedMu.Unlock()
	if repliedText != "Channel reply" {
		t.Errorf("repliedText = %q, want %q", repliedText, "Channel reply")
	}
	if repliedDelivery == nil || repliedDelivery.Channel != "telegram" {
		t.Errorf("repliedDelivery = %+v, want channel=telegram", repliedDelivery)
	}
}

// TestIntegration_InterruptPreviousRun tests that sending a new message
// to the same session interrupts the previous run.
func TestIntegration_InterruptPreviousRun(t *testing.T) {
	// First request: LLM blocks forever.
	// Second request: LLM responds immediately.
	callCount := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if n == 1 {
			flusher, _ := w.(http.Flusher)
			fmt.Fprint(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0}}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, sseResponse("Interrupted result", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h, sm, _, _ := newIntegrationHandler(server, transcript)
	defer h.Close()

	sessionKey := "int-interrupt-1"

	// Start first (blocking) run.
	req1 := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "first",
		"clientRunId": "run-i-1",
	})
	h.Send(context.Background(), req1)

	// Wait for it to start.
	waitForSessionStatus(sm, sessionKey, session.StatusRunning, 2*time.Second)

	// Send a second message via sessions.send (interrupts the first run).
	req2 := makeReq("2", "sessions.send", map[string]any{
		"key":     sessionKey,
		"message": "second",
	})
	h.SessionsSend(context.Background(), req2)

	// Wait for the second run to complete.
	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want %q", status, session.StatusDone)
	}
}

// TestIntegration_LLMError tests that an LLM error transitions the session
// to failed and emits error events.
func TestIntegration_LLMError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": {"message": "internal server error"}}`)
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}

	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "int-err-1",
		"message":     "hello",
		"clientRunId": "run-err-1",
	})
	h.Send(context.Background(), req)

	// Session should transition to failed.
	status := waitForSessionStatus(sm, "int-err-1", session.StatusFailed, 5*time.Second)
	if status != session.StatusFailed {
		t.Fatalf("session status = %q, want %q", status, session.StatusFailed)
	}

	// Verify error broadcast.
	events := bc.get()
	hasError := false
	for _, ev := range events {
		if ev.Event == "sessions.changed" {
			p, ok := ev.Payload.(map[string]any)
			if ok && p["status"] == "failed" {
				hasError = true
			}
		}
	}
	if !hasError {
		t.Error("missing sessions.changed failed event")
	}
}

// TestIntegration_SlashCommandReset tests that /reset clears the transcript
// and transitions the session.
func TestIntegration_SlashCommandReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("reply", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	h, sm, _, _ := newIntegrationHandler(server, transcript)
	defer h.Close()

	sessionKey := "int-reset-1"

	// Send a message first.
	sendReq := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "hello",
		"clientRunId": "run-r-1",
	})
	h.Send(context.Background(), sendReq)
	waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)

	// Verify transcript exists.
	msgs, _, _ := transcript.Load(sessionKey, 0)
	if len(msgs) == 0 {
		t.Fatal("expected transcript after send")
	}

	// Send /reset.
	resetReq := makeReq("2", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "/reset",
		"clientRunId": "run-r-2",
	})
	resetResp := h.Send(context.Background(), resetReq)
	if !resetResp.OK {
		t.Fatalf("reset failed: %v", resetResp.Error)
	}

	// Transcript should be cleared.
	msgs, _, _ = transcript.Load(sessionKey, 0)
	if len(msgs) != 0 {
		t.Errorf("expected empty transcript after reset, got %d messages", len(msgs))
	}
}
