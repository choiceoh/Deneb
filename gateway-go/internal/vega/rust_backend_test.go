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

	// Result should be valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Result is not valid JSON: %v", err)
	}

	// Without Vega feature: returns {"error":"vega backend unavailable ..."}
	// With Vega feature: returns actual command result
	t.Logf("Execute result: %s", string(result))
}

func TestRustBackendSearch(t *testing.T) {
	rb := NewRustBackend(RustBackendConfig{})

	results, err := rb.Search(context.Background(), "test query", SearchOpts{Limit: 5})
	if err != nil {
		// Without Vega feature, search returns empty results or error.
		t.Logf("Search returned error (expected without Vega feature): %v", err)
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
