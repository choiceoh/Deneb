package ffi

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestCompactionEvaluate_ShouldCompact(t *testing.T) {
	config := `{"contextThreshold":0.75}`
	result, err := CompactionEvaluate(config, 8000, 9000, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decision struct {
		ShouldCompact bool   `json:"shouldCompact"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal(result, &decision); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !decision.ShouldCompact {
		t.Error("expected shouldCompact=true when tokens exceed threshold")
	}
	t.Logf("decision: %s", string(result))
}

func TestCompactionEvaluate_NoCompaction(t *testing.T) {
	config := `{"contextThreshold":0.75}`
	result, err := CompactionEvaluate(config, 3000, 2000, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decision struct {
		ShouldCompact bool `json:"shouldCompact"`
	}
	if err := json.Unmarshal(result, &decision); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decision.ShouldCompact {
		t.Error("expected shouldCompact=false when tokens are under threshold")
	}
}

func TestCompactionEvaluate_EmptyConfig(t *testing.T) {
	_, err := CompactionEvaluate("", 1000, 1000, 10000)
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestCompactionSweepNew_Basic(t *testing.T) {
	config := `{"contextThreshold":0.75}`
	handle, err := CompactionSweepNew(config, 1, 10000, false, false, 1700000000000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle == 0 {
		t.Fatal("expected non-zero handle")
	}
	defer CompactionSweepDrop(handle)
}

func TestCompactionSweepNew_EmptyConfig(t *testing.T) {
	_, err := CompactionSweepNew("", 1, 10000, false, false, 1700000000000)
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestCompactionSweepStart_Roundtrip(t *testing.T) {
	config := `{"contextThreshold":0.75}`
	handle, err := CompactionSweepNew(config, 1, 10000, false, false, 1700000000000)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer CompactionSweepDrop(handle)

	cmd, err := CompactionSweepStart(handle)
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
	t.Logf("sweep start command: %s", string(cmd))
}

func TestCompactionSweepStep_EmptyResponse(t *testing.T) {
	config := `{"contextThreshold":0.75}`
	handle, err := CompactionSweepNew(config, 1, 10000, false, false, 1700000000000)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer CompactionSweepDrop(handle)

	_, err = CompactionSweepStep(handle, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestCompactionSweepDrop_ZeroHandle(t *testing.T) {
	// Dropping handle 0 should be a safe no-op.
	CompactionSweepDrop(0)
}

// TestCompactionSweep_ConcurrentHandles verifies that multiple sweep handles
// can be created, used, and dropped concurrently without races or panics.
func TestCompactionSweep_ConcurrentHandles(t *testing.T) {
	const goroutines = 8
	config := `{"contextThreshold":0.75}`

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			handle, err := CompactionSweepNew(config, uint64(id+1), 10000, false, false, 1700000000000)
			if err != nil {
				t.Errorf("goroutine %d: sweep new failed: %v", id, err)
				return
			}
			defer CompactionSweepDrop(handle)

			if _, err := CompactionSweepStart(handle); err != nil {
				t.Errorf("goroutine %d: sweep start failed: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
}
