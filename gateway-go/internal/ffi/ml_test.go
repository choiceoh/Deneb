package ffi

import (
	"encoding/json"
	"testing"
)

func TestMLEmbed_Stub(t *testing.T) {
	result, err := MLEmbed(`{"texts":["hello world"]}`)
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
	// Phase 0 stub should include "embeddings" key.
	if _, ok := parsed["embeddings"]; !ok {
		t.Error("expected 'embeddings' key in response")
	}
	t.Logf("MLEmbed result: %s", string(result))
}

func TestMLRerank_Stub(t *testing.T) {
	result, err := MLRerank(`{"query":"test","documents":["a","b"]}`)
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
	// Phase 0 stub should include "ranked" key.
	if _, ok := parsed["ranked"]; !ok {
		t.Error("expected 'ranked' key in response")
	}
	t.Logf("MLRerank result: %s", string(result))
}

func TestMLEmbed_Empty(t *testing.T) {
	_, err := MLEmbed("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestMLRerank_Empty(t *testing.T) {
	_, err := MLRerank("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}
