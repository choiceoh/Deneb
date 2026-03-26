package chat

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTurnContext_StoreAndLoad(t *testing.T) {
	tc := NewTurnContext()

	tc.Store("toolu_1", &turnResult_{
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
	tc.Store("toolu_1", &turnResult_{ToolName: "grep", Output: "match"})

	r, ok := tc.Wait("toolu_1", 1*time.Second)
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

	var result *turnResult_
	var ok bool
	go func() {
		defer wg.Done()
		result, ok = tc.Wait("toolu_1", 5*time.Second)
	}()

	// Simulate delayed store.
	time.Sleep(50 * time.Millisecond)
	tc.Store("toolu_1", &turnResult_{ToolName: "exec", Output: "done"})

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

	r, ok := tc.Wait("toolu_never", 50*time.Millisecond)
	if ok {
		t.Fatal("expected ok=false on timeout")
	}
	if r != nil {
		t.Error("expected nil result on timeout")
	}
}

func TestTurnContext_ConcurrentAccess(t *testing.T) {
	tc := NewTurnContext()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "toolu_" + string(rune('a'+id))
			tc.Store(key, &turnResult_{ToolName: "test", Output: key})
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
	if !contains(err.Error(), "circular") {
		t.Errorf("expected 'circular' in error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
