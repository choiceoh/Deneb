package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// TestDualCore_ConcurrentResponseWhileTaskRunning verifies that a new message
// during an active task run goes to the concurrent response path instead of
// aborting the task.
func TestDualCore_ConcurrentResponseWhileTaskRunning(t *testing.T) {
	var taskCallCount int
	var concCallCount int
	var mu sync.Mutex

	taskStarted := make(chan struct{})
	taskRelease := make(chan struct{})
	concReplied := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var reqBody struct {
			Tools []json.RawMessage `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		mu.Lock()
		if len(reqBody.Tools) > 0 {
			taskCallCount++
			mu.Unlock()
			close(taskStarted)
			select {
			case <-taskRelease:
				fmt.Fprint(w, sseResponse("Task completed!", "end_turn"))
			case <-r.Context().Done():
				return
			}
		} else {
			concCallCount++
			mu.Unlock()
			fmt.Fprint(w, sseResponse("지금 작업 중이에요, 잠깐만요!", "end_turn"))
		}
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	tools := NewToolRegistry()
	tools.Register("dummy", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	replyFn := func(_ context.Context, _ *DeliveryContext, text string) error {
		select {
		case concReplied <- text:
		default:
		}
		return nil
	}

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.Tools = tools
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	h.SetReplyFunc(replyFn)
	defer h.Close()

	sessionKey := "dual-core-test"
	delivery := map[string]any{"channel": "telegram", "to": "123"}

	// Step 1: Start a task (blocks at LLM server).
	resp1 := h.Send(context.Background(), makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "do some long task",
		"clientRunId": "run-task-1",
		"delivery":    delivery,
	}))
	if !resp1.OK {
		t.Fatalf("Send (task) failed: %v", resp1.Error)
	}

	select {
	case <-taskStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	if tp := h.getTaskProgress(sessionKey); tp == nil {
		t.Fatal("expected task progress to be registered")
	}

	// Step 2: Send concurrent message while task runs.
	resp2 := h.Send(context.Background(), makeReq("2", "chat.send", map[string]any{
		"sessionKey": sessionKey,
		"message":    "지금 뭐하고 있어?",
		"delivery":   delivery,
	}))
	if !resp2.OK {
		t.Fatalf("Send (concurrent) failed: %v", resp2.Error)
	}

	var resp2Payload map[string]any
	json.Unmarshal(resp2.Payload, &resp2Payload)
	if resp2Payload["mode"] != "concurrent_response" {
		t.Errorf("concurrent message mode = %v, want concurrent_response", resp2Payload["mode"])
	}

	// Wait for concurrent response via replyFunc (channel-based, not sleep).
	select {
	case reply := <-concReplied:
		if reply != "지금 작업 중이에요, 잠깐만요!" {
			t.Errorf("concurrent reply = %q, want expected text", reply)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for concurrent response reply")
	}

	// Step 3: Release task and verify completion.
	close(taskRelease)

	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want done", status)
	}

	mu.Lock()
	tc, cc := taskCallCount, concCallCount
	mu.Unlock()
	if tc < 1 {
		t.Errorf("task LLM calls = %d, want >= 1", tc)
	}
	if cc < 1 {
		t.Errorf("concurrent LLM calls = %d, want >= 1", cc)
	}

	// Verify transcript ordering:
	// user(task) → user(concurrent) → assistant(concurrent) → assistant(task)
	msgs, _, err := transcript.Load(sessionKey, 0)
	if err != nil {
		t.Fatalf("transcript load: %v", err)
	}
	if len(msgs) < 4 {
		t.Fatalf("transcript has %d messages, want >= 4", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "do some long task" {
		t.Errorf("msgs[0] = {%s, %q}, want {user, do some long task}", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "지금 뭐하고 있어?" {
		t.Errorf("msgs[1] = {%s, %q}, want {user, 지금 뭐하고 있어?}", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "지금 작업 중이에요, 잠깐만요!" {
		t.Errorf("msgs[2] = {%s, %q}, want {assistant, 지금 작업 중이에요, 잠깐만요!}", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "Task completed!" {
		t.Errorf("msgs[3] = {%s, %q}, want {assistant, Task completed!}", msgs[3].Role, msgs[3].Content)
	}
}

// TestDualCore_ExplicitInterruptCancelsTask verifies that an explicit interrupt
// keyword ("그만") cancels the running task.
func TestDualCore_ExplicitInterruptCancelsTask(t *testing.T) {
	taskStarted := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var reqBody struct {
			Tools []json.RawMessage `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		if len(reqBody.Tools) > 0 {
			close(taskStarted)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, sseResponse("새 작업 시작!", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	tools := NewToolRegistry()
	tools.Register("dummy", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.Tools = tools
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	sessionKey := "interrupt-test"

	h.Send(context.Background(), makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "long task",
		"clientRunId": "run-long-1",
	}))

	select {
	case <-taskStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	// "그만" triggers interrupt, NOT concurrent response.
	resp2 := h.Send(context.Background(), makeReq("2", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "그만",
		"clientRunId": "run-stop-1",
	}))

	var payload map[string]any
	json.Unmarshal(resp2.Payload, &payload)
	if payload["mode"] == "concurrent_response" {
		t.Error("explicit interrupt should NOT route to concurrent_response")
	}

	// New run should complete (the interrupt starts a new task).
	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want done after interrupt", status)
	}
}

// TestDualCore_ConcurrentResponseCancellation verifies that sending multiple
// concurrent messages cancels the previous concurrent response.
func TestDualCore_ConcurrentResponseCancellation(t *testing.T) {
	taskStarted := make(chan struct{}, 1)
	concStarted := make(chan struct{}, 5)
	var concCallCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var reqBody struct {
			Tools []json.RawMessage `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		if len(reqBody.Tools) > 0 {
			select {
			case taskStarted <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}

		// Signal that a concurrent response LLM call started.
		mu.Lock()
		concCallCount++
		mu.Unlock()
		select {
		case concStarted <- struct{}{}:
		default:
		}

		// Delay to give time for cancellation to arrive.
		select {
		case <-time.After(2 * time.Second):
			fmt.Fprint(w, sseResponse("응답", "end_turn"))
		case <-r.Context().Done():
			return // cancelled
		}
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	sm := session.NewManager()
	bc := &broadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	tools := NewToolRegistry()
	tools.Register("dummy", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.Tools = tools
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "Test assistant"

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	sessionKey := "cancel-conc-test"

	// Start task.
	h.Send(context.Background(), makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "task",
		"clientRunId": "run-task",
	}))

	<-taskStarted

	// Send first concurrent message.
	h.Send(context.Background(), makeReq("2", "chat.send", map[string]any{
		"sessionKey": sessionKey, "message": "질문 1",
	}))

	// Wait for first concurrent response to actually reach the LLM server.
	select {
	case <-concStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for first concurrent response to start")
	}

	// Send second concurrent message — should cancel the first.
	h.Send(context.Background(), makeReq("3", "chat.send", map[string]any{
		"sessionKey": sessionKey, "message": "질문 2",
	}))

	// Wait for second concurrent response to start.
	select {
	case <-concStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for second concurrent response to start")
	}

	// Both concurrent responses started (2 LLM calls).
	mu.Lock()
	calls := concCallCount
	mu.Unlock()
	if calls < 2 {
		t.Errorf("concurrent LLM calls = %d, want >= 2", calls)
	}
}
