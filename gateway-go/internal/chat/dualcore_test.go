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

	// LLM server that:
	// - First request: blocks to simulate a long-running task, then responds.
	// - Second request (concurrent response): responds immediately.
	taskStarted := make(chan struct{})
	taskRelease := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Parse the request to check if it's a tool-less call (concurrent response)
		// by checking the tools field.
		var reqBody struct {
			Tools []json.RawMessage `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&reqBody)

		mu.Lock()
		if len(reqBody.Tools) > 0 {
			// Task core call (has tools).
			taskCallCount++
			mu.Unlock()
			close(taskStarted) // signal that task has started
			select {
			case <-taskRelease:
				fmt.Fprint(w, sseResponse("Task completed!", "end_turn"))
			case <-r.Context().Done():
				return
			}
		} else {
			// Concurrent response call (no tools).
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
	// Register a dummy tool so the task core gets tools in its config.
	tools.Register("dummy", func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	var replyMu sync.Mutex
	var replies []string
	replyFn := func(_ context.Context, _ *DeliveryContext, text string) error {
		replyMu.Lock()
		replies = append(replies, text)
		replyMu.Unlock()
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

	// Step 1: Start a task (which will block at the LLM server).
	req1 := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "do some long task",
		"clientRunId": "run-task-1",
		"delivery":    delivery,
	})
	resp1 := h.Send(context.Background(), req1)
	if !resp1.OK {
		t.Fatalf("Send (task) failed: %v", resp1.Error)
	}

	// Wait for the task to actually start (LLM server receives the request).
	select {
	case <-taskStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	// Verify task progress tracker is registered.
	if tp := h.getTaskProgress(sessionKey); tp == nil {
		t.Fatal("expected task progress to be registered")
	}

	// Step 2: Send a concurrent message while the task is running.
	req2 := makeReq("2", "chat.send", map[string]any{
		"sessionKey": sessionKey,
		"message":    "지금 뭐하고 있어?",
		"delivery":   delivery,
	})
	resp2 := h.Send(context.Background(), req2)
	if !resp2.OK {
		t.Fatalf("Send (concurrent) failed: %v", resp2.Error)
	}

	// Verify it was routed to concurrent response (mode field in response).
	var resp2Payload map[string]any
	json.Unmarshal(resp2.Payload, &resp2Payload)
	if resp2Payload["mode"] != "concurrent_response" {
		t.Errorf("concurrent message mode = %v, want concurrent_response", resp2Payload["mode"])
	}

	// Wait for concurrent response to complete (it should be fast).
	time.Sleep(2 * time.Second)

	// Verify concurrent response was delivered.
	replyMu.Lock()
	concReplies := len(replies)
	replyMu.Unlock()
	if concReplies < 1 {
		t.Error("expected at least 1 concurrent response reply")
	}

	// Step 3: Release the task and let it complete.
	close(taskRelease)

	// Wait for task to complete.
	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want done", status)
	}

	// Verify both calls happened.
	mu.Lock()
	tc, cc := taskCallCount, concCallCount
	mu.Unlock()
	if tc < 1 {
		t.Errorf("task LLM calls = %d, want >= 1", tc)
	}
	if cc < 1 {
		t.Errorf("concurrent LLM calls = %d, want >= 1", cc)
	}

	// Verify transcript contains all messages in order.
	msgs, _, err := transcript.Load(sessionKey, 0)
	if err != nil {
		t.Fatalf("transcript load: %v", err)
	}
	// Expected: user(task) + user(concurrent) + assistant(concurrent) + assistant(task)
	if len(msgs) < 4 {
		t.Fatalf("transcript has %d messages, want >= 4", len(msgs))
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
			// Task core: block until cancelled.
			close(taskStarted)
			<-r.Context().Done()
			return
		}
		// Post-interrupt task: respond normally.
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

	// Start a long task.
	req1 := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "long task",
		"clientRunId": "run-long-1",
	})
	h.Send(context.Background(), req1)

	select {
	case <-taskStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task to start")
	}

	// Send "그만" — should trigger interrupt.
	req2 := makeReq("2", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "그만",
		"clientRunId": "run-stop-1",
	})
	resp2 := h.Send(context.Background(), req2)

	// Should NOT be concurrent_response mode.
	var payload map[string]any
	json.Unmarshal(resp2.Payload, &payload)
	if payload["mode"] == "concurrent_response" {
		t.Error("explicit interrupt should NOT route to concurrent_response")
	}

	// Task progress should be cleared after interrupt.
	time.Sleep(500 * time.Millisecond)
	// The new run takes over the session.
	status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second)
	if status != session.StatusDone {
		t.Fatalf("session status = %q, want done after interrupt", status)
	}
}

// TestDualCore_ConcurrentResponseCancellation verifies that sending multiple
// concurrent messages cancels the previous concurrent response.
func TestDualCore_ConcurrentResponseCancellation(t *testing.T) {
	taskStarted := make(chan struct{}, 1)
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
			// Block forever (task never completes in this test).
			<-r.Context().Done()
			return
		}
		// Concurrent response: slight delay then respond.
		mu.Lock()
		concCallCount++
		mu.Unlock()

		select {
		case <-time.After(500 * time.Millisecond):
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

	// Send two concurrent messages rapidly — second should cancel first.
	h.Send(context.Background(), makeReq("2", "chat.send", map[string]any{
		"sessionKey": sessionKey, "message": "질문 1",
	}))
	// Small delay to ensure first concurrent response starts.
	time.Sleep(50 * time.Millisecond)
	h.Send(context.Background(), makeReq("3", "chat.send", map[string]any{
		"sessionKey": sessionKey, "message": "질문 2",
	}))

	// Wait for the second concurrent response to complete.
	time.Sleep(3 * time.Second)

	mu.Lock()
	calls := concCallCount
	mu.Unlock()

	// Both concurrent responses should have started (2 LLM calls),
	// but the first one should have been cancelled.
	if calls < 2 {
		t.Errorf("concurrent LLM calls = %d, want >= 2", calls)
	}
}
