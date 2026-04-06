package ffi

import (
	"encoding/json"
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

func TestCompactionSweepNew_Unavailable(t *testing.T) {
	_, err := CompactionSweepNew(`{"contextThreshold":0.75}`, 1, 10000, false, false, 1700000000000)
	if err == nil {
		t.Error("expected error since Rust FFI is removed")
	}
}

func TestCompactionSweepDrop_ZeroHandle(t *testing.T) {
	// Dropping handle 0 should be a safe no-op.
	CompactionSweepDrop(0)
}
