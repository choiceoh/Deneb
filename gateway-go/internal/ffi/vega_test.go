package ffi

import (
	"encoding/json"
	"testing"
)

func TestVegaExecute_Stub(t *testing.T) {
	result, err := VegaExecute(`{"command":"list"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	// Phase 0 stub should include "phase":0.
	if phase, ok := parsed["phase"]; ok {
		if phase != float64(0) {
			t.Errorf("expected phase=0, got %v", phase)
		}
	}
	t.Logf("VegaExecute result: %s", string(result))
}

func TestVegaSearch_Stub(t *testing.T) {
	result, err := VegaSearch(`{"query":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	// Phase 0 stub should include "results" key.
	if _, ok := parsed["results"]; !ok {
		t.Error("expected 'results' key in response")
	}
	t.Logf("VegaSearch result: %s", string(result))
}

func TestVegaExecute_Empty(t *testing.T) {
	_, err := VegaExecute("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestVegaSearch_Empty(t *testing.T) {
	_, err := VegaSearch("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}
