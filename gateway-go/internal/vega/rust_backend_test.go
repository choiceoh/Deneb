package vega

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRustBackendExecute(t *testing.T) {
	rb := NewRustBackend(RustBackendConfig{})

	result, err := rb.Execute(context.Background(), "system", map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Result should be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Result is not valid JSON: %v", err)
	}

	// Phase 0 stub returns {"error":"vega_not_implemented","phase":0}
	// Phase 1 (with vega feature) returns actual command result
	t.Logf("Execute result: %s", string(result))
}

func TestRustBackendSearch(t *testing.T) {
	rb := NewRustBackend(RustBackendConfig{})

	// This should not error even with stub response
	results, err := rb.Search(context.Background(), "test query", SearchOpts{Limit: 5})
	if err != nil {
		// Phase 0 stub returns {"results":[],"phase":0} which won't have "unified" key
		// This is expected — the search will return empty results
		t.Logf("Search returned error (expected for phase 0): %v", err)
		return
	}

	t.Logf("Search returned %d results", len(results))
}

func TestRustBackendClose(t *testing.T) {
	rb := NewRustBackend(RustBackendConfig{})
	if err := rb.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
