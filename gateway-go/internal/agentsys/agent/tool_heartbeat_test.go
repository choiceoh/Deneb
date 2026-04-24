package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// slowToolExecutor sleeps for execTime before returning, letting tests
// verify that OnToolProgress fires periodically during a long tool call.
type slowToolExecutor struct {
	execTime time.Duration
}

func (s *slowToolExecutor) Execute(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(s.execTime):
		return "done", nil
	}
}

// TestToolHeartbeat_FiresDuringLongTool verifies that OnToolProgress fires
// periodically while a tool is still executing, so surface liveness
// indicators (Telegram typing "...") stay alive during multi-minute tools.
func TestToolHeartbeat_FiresDuringLongTool(t *testing.T) {
	// Shrink the interval so the test runs in under a second.
	origInterval := toolHeartbeatInterval
	toolHeartbeatInterval = 50 * time.Millisecond
	t.Cleanup(func() { toolHeartbeatInterval = origInterval })

	var (
		mu            sync.Mutex
		progressCalls []int // elapsed seconds for each fire
	)
	hooks := StreamHooks{
		OnToolProgress: func(_, _ string, elapsedSec int) {
			mu.Lock()
			progressCalls = append(progressCalls, elapsedSec)
			mu.Unlock()
		},
	}

	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "tool_1",
		Name:  "fake_slow_tool",
		Input: json.RawMessage(`{}`),
	}
	tools := &slowToolExecutor{execTime: 250 * time.Millisecond}

	// executeOneTool runs the single tool call. With execTime=250ms and
	// interval=50ms, we expect ~4-5 progress fires.
	block := executeOneTool(context.Background(), tc, tools, hooks,
		"", 0, slog.Default(), nil, nil)

	if block.IsError {
		t.Fatalf("unexpected tool error: %q", block.Content)
	}

	mu.Lock()
	fires := len(progressCalls)
	mu.Unlock()

	if fires < 2 {
		t.Fatalf("OnToolProgress fired %d times, want >= 2 for a 250ms tool with 50ms interval", fires)
	}

	// Heartbeat must stop promptly after the tool returns. Wait for a few
	// more ticks and verify no additional fires landed.
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	postFires := len(progressCalls)
	mu.Unlock()

	if postFires != fires {
		t.Fatalf("OnToolProgress kept firing after tool return: was %d, now %d", fires, postFires)
	}
}

// TestToolHeartbeat_ShortToolNoFire verifies no heartbeat fires when the
// tool returns faster than the first tick.
func TestToolHeartbeat_ShortToolNoFire(t *testing.T) {
	origInterval := toolHeartbeatInterval
	toolHeartbeatInterval = 100 * time.Millisecond
	t.Cleanup(func() { toolHeartbeatInterval = origInterval })

	var fires int32
	hooks := StreamHooks{
		OnToolProgress: func(_, _ string, _ int) {
			atomic.AddInt32(&fires, 1)
		},
	}

	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "tool_1",
		Name:  "fake_fast_tool",
		Input: json.RawMessage(`{}`),
	}
	tools := &slowToolExecutor{execTime: 10 * time.Millisecond}

	_ = executeOneTool(context.Background(), tc, tools, hooks,
		"", 0, slog.Default(), nil, nil)

	// Give any in-flight heartbeat goroutine a chance to fire.
	time.Sleep(150 * time.Millisecond)

	if n := atomic.LoadInt32(&fires); n != 0 {
		t.Fatalf("OnToolProgress fired %d times for a 10ms tool, want 0", n)
	}
}

// TestToolHeartbeat_NilHookIsSafe verifies the heartbeat goroutine is not
// spawned when OnToolProgress is nil, so we don't pay goroutine cost for
// every tool call when the caller has no interest in progress signals.
func TestToolHeartbeat_NilHookIsSafe(t *testing.T) {
	origInterval := toolHeartbeatInterval
	toolHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { toolHeartbeatInterval = origInterval })

	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "tool_1",
		Name:  "fake_tool",
		Input: json.RawMessage(`{}`),
	}
	tools := &slowToolExecutor{execTime: 50 * time.Millisecond}

	// No OnToolProgress in hooks — heartbeat goroutine should not spawn.
	hooks := StreamHooks{}
	block := executeOneTool(context.Background(), tc, tools, hooks,
		"", 0, slog.Default(), nil, nil)

	if block.IsError {
		t.Fatalf("unexpected tool error: %q", block.Content)
	}
}

// TestToolHeartbeat_ConcurrentToolsIndependent verifies that two tools
// executed back-to-back each get their own heartbeat scoped to their own
// execution window — no state bleed between calls.
func TestToolHeartbeat_ConcurrentToolsIndependent(t *testing.T) {
	origInterval := toolHeartbeatInterval
	toolHeartbeatInterval = 30 * time.Millisecond
	t.Cleanup(func() { toolHeartbeatInterval = origInterval })

	var (
		mu       sync.Mutex
		firesPer = map[string]int{}
	)
	hooks := StreamHooks{
		OnToolProgress: func(_, toolUseID string, _ int) {
			mu.Lock()
			firesPer[toolUseID]++
			mu.Unlock()
		},
	}

	tools := &slowToolExecutor{execTime: 150 * time.Millisecond}
	for _, id := range []string{"tool_a", "tool_b"} {
		tc := llm.ContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  "fake_tool",
			Input: json.RawMessage(`{}`),
		}
		_ = executeOneTool(context.Background(), tc, tools, hooks,
			"", 0, slog.Default(), nil, nil)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"tool_a", "tool_b"} {
		if n := firesPer[id]; n < 2 {
			t.Fatalf("tool %q got %d progress fires, want >= 2", id, n)
		}
	}
}
