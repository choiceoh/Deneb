package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTurnContext_StoreAndLoad(t *testing.T) {
	tc := NewTurnContext()

	tc.Store("toolu_1", &TurnResult{
		ToolName: "read",
		Output:   "file contents",
		IsError:  false,
		Duration: 50 * time.Millisecond,
	})

	r := tc.Load("toolu_1")
	if r == nil {
		t.Fatal("expected result, got nil")
	}
	if r.ToolName != "read" {
		t.Errorf("expected tool name 'read', got %q", r.ToolName)
	}
	if r.Output != "file contents" {
		t.Errorf("expected output 'file contents', got %q", r.Output)
	}

	// Non-existent key.
	if tc.Load("toolu_999") != nil {
		t.Error("expected nil for non-existent key")
	}
}

func TestTurnContext_Wait_AlreadyAvailable(t *testing.T) {
	tc := NewTurnContext()
	tc.Store("toolu_1", &TurnResult{ToolName: "grep", Output: "match"})

	r, ok := tc.Wait(context.Background(), "toolu_1", 1*time.Second)
	if !ok {
		t.Fatal("expected ok=true for already-available result")
	}
	if r.Output != "match" {
		t.Errorf("expected output 'match', got %q", r.Output)
	}
}

func TestTurnContext_Wait_BlocksUntilStored(t *testing.T) {
	tc := NewTurnContext()

	var wg sync.WaitGroup
	wg.Add(1)

	var result *TurnResult
	var ok bool
	go func() {
		defer wg.Done()
		result, ok = tc.Wait(context.Background(), "toolu_1", 5*time.Second)
	}()

	// Simulate delayed store.
	time.Sleep(50 * time.Millisecond)
	tc.Store("toolu_1", &TurnResult{ToolName: "exec", Output: "done"})

	wg.Wait()
	if !ok {
		t.Fatal("expected ok=true after store")
	}
	if result.Output != "done" {
		t.Errorf("expected 'done', got %q", result.Output)
	}
}

func TestTurnContext_Wait_Timeout(t *testing.T) {
	tc := NewTurnContext()

	r, ok := tc.Wait(context.Background(), "toolu_never", 50*time.Millisecond)
	if ok {
		t.Fatal("expected ok=false on timeout")
	}
	if r != nil {
		t.Error("expected nil result on timeout")
	}
}

func TestTurnContext_Wait_ContextCancelled(t *testing.T) {
	tc := NewTurnContext()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay — should return before the 5s timeout.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	r, ok := tc.Wait(ctx, "toolu_never", 5*time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected ok=false on context cancellation")
	}
	if r != nil {
		t.Error("expected nil result on context cancellation")
	}
	if elapsed > 1*time.Second {
		t.Errorf("Wait should have returned quickly on cancel, took %v", elapsed)
	}
}

func TestTurnContext_ConcurrentAccess(t *testing.T) {
	tc := NewTurnContext()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "toolu_" + string(rune('a'+id))
			tc.Store(key, &TurnResult{ToolName: "test", Output: key})
			tc.Load(key)
		}(i)
	}
	wg.Wait()

	ids := tc.IDs()
	if len(ids) != 20 {
		t.Errorf("expected 20 stored results, got %d", len(ids))
	}
}

func TestTurnContext_ContextIntegration(t *testing.T) {
	tc := NewTurnContext()
	ctx := WithTurnContext(context.Background(), tc)

	extracted := TurnContextFromContext(ctx)
	if extracted != tc {
		t.Error("expected same TurnContext from context")
	}

	// Nil context.
	if TurnContextFromContext(context.Background()) != nil {
		t.Error("expected nil from context without TurnContext")
	}
}

func TestDetectCycle_NoCycle(t *testing.T) {
	refs := map[string]string{
		"toolu_2": "toolu_1",
		"toolu_3": "toolu_2",
	}
	if err := DetectCycle(refs); err != nil {
		t.Errorf("expected no cycle, got: %v", err)
	}
}

func TestDetectCycle_WithCycle(t *testing.T) {
	refs := map[string]string{
		"toolu_1": "toolu_2",
		"toolu_2": "toolu_3",
		"toolu_3": "toolu_1",
	}
	err := DetectCycle(refs)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected 'circular' in error, got: %v", err)
	}
}
