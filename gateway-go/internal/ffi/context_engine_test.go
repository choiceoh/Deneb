//go:build !no_ffi

package ffi

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestContextAssemblyNew_Basic(t *testing.T) {
	handle, err := ContextAssemblyNew(1, 4096, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle == 0 {
		t.Fatal("expected non-zero handle")
	}
	defer ContextEngineDrop(handle)
}

func TestContextAssemblyStart_Roundtrip(t *testing.T) {
	handle, err := ContextAssemblyNew(1, 4096, 10)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer ContextEngineDrop(handle)

	cmd, err := ContextAssemblyStart(handle)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if len(cmd) == 0 {
		t.Fatal("expected non-empty command JSON")
	}

	// Verify valid JSON output.
	var parsed map[string]any
	if err := json.Unmarshal(cmd, &parsed); err != nil {
		t.Fatalf("invalid JSON from start: %v", err)
	}
	t.Logf("assembly start command: %s", string(cmd))
}

func TestContextAssemblyStep_EmptyResponse(t *testing.T) {
	handle, err := ContextAssemblyNew(1, 4096, 10)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer ContextEngineDrop(handle)

	_, err = ContextAssemblyStep(handle, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestContextExpandNew_Basic(t *testing.T) {
	handle, err := ContextExpandNew("summary-001", 3, true, 8192)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle == 0 {
		t.Fatal("expected non-zero handle")
	}
	defer ContextEngineDrop(handle)
}

func TestContextExpandNew_EmptySummaryID(t *testing.T) {
	_, err := ContextExpandNew("", 3, true, 8192)
	if err == nil {
		t.Error("expected error for empty summary_id")
	}
}

func TestContextExpandStart_Roundtrip(t *testing.T) {
	handle, err := ContextExpandNew("summary-001", 3, false, 8192)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer ContextEngineDrop(handle)

	cmd, err := ContextExpandStart(handle)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if len(cmd) == 0 {
		t.Fatal("expected non-empty command JSON")
	}

	var parsed map[string]any
	if err := json.Unmarshal(cmd, &parsed); err != nil {
		t.Fatalf("invalid JSON from start: %v", err)
	}
	t.Logf("expand start command: %s", string(cmd))
}

func TestContextExpandStep_EmptyResponse(t *testing.T) {
	handle, err := ContextExpandNew("summary-001", 3, true, 8192)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer ContextEngineDrop(handle)

	_, err = ContextExpandStep(handle, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestContextEngineDrop_ZeroHandle(t *testing.T) {
	// Dropping handle 0 should be a safe no-op.
	ContextEngineDrop(0)
}

// TestContextEngine_ConcurrentHandles verifies that multiple handles can be
// created, used, and dropped concurrently without races or panics.
func TestContextEngine_ConcurrentHandles(t *testing.T) {
	const goroutines = 8

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Assembly path.
			ah, err := ContextAssemblyNew(uint64(id), 2048, 5)
			if err != nil {
				t.Errorf("goroutine %d: assembly new failed: %v", id, err)
				return
			}
			defer ContextEngineDrop(ah)

			if _, err := ContextAssemblyStart(ah); err != nil {
				t.Errorf("goroutine %d: assembly start failed: %v", id, err)
			}

			// Expand path.
			eh, err := ContextExpandNew("sum-concurrent", 2, false, 4096)
			if err != nil {
				t.Errorf("goroutine %d: expand new failed: %v", id, err)
				return
			}
			defer ContextEngineDrop(eh)

			if _, err := ContextExpandStart(eh); err != nil {
				t.Errorf("goroutine %d: expand start failed: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
}
