package embedding

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// NewGeminiEmbedder
// ---------------------------------------------------------------------------

func TestNewGeminiEmbedder_emptyAPIKey(t *testing.T) {
	e := NewGeminiEmbedder("", nil)
	if e != nil {
		t.Fatal("expected nil for empty API key")
	}
}

func TestNewGeminiEmbedder_withKey(t *testing.T) {
	e := NewGeminiEmbedder("test-key", nil)
	if e == nil {
		t.Fatal("expected non-nil embedder")
	}
	if e.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", e.apiKey, "test-key")
	}
}

func TestNewGeminiEmbedder_nilLoggerFallback(t *testing.T) {
	e := NewGeminiEmbedder("key", nil)
	if e.logger == nil {
		t.Fatal("expected non-nil logger from slog.Default() fallback")
	}
}

// ---------------------------------------------------------------------------
// toFloat32
// ---------------------------------------------------------------------------

func TestToFloat32_conversion(t *testing.T) {
	in := []float64{1.0, 2.5, -0.5}
	out := toFloat32(in)
	if len(out) != len(in) {
		t.Fatalf("len mismatch: got %d, want %d", len(out), len(in))
	}
	for i, v := range in {
		if float64(out[i]) != v {
			t.Errorf("[%d]: got %v, want %v", i, out[i], v)
		}
	}
}

func TestToFloat32_empty(t *testing.T) {
	out := toFloat32(nil)
	if len(out) != 0 {
		t.Errorf("expected empty slice, got len=%d", len(out))
	}
}

// ---------------------------------------------------------------------------
// l2Normalize
// ---------------------------------------------------------------------------

func TestL2Normalize_unitVector(t *testing.T) {
	vec := []float32{3.0, 4.0}
	l2Normalize(vec)
	// Expected: [0.6, 0.8] (norm=5)
	if math.Abs(float64(vec[0])-0.6) > 1e-6 {
		t.Errorf("vec[0] = %v, want ~0.6", vec[0])
	}
	if math.Abs(float64(vec[1])-0.8) > 1e-6 {
		t.Errorf("vec[1] = %v, want ~0.8", vec[1])
	}
}

func TestL2Normalize_zeroVector(t *testing.T) {
	vec := []float32{0.0, 0.0}
	l2Normalize(vec) // must not panic or divide by zero
	if vec[0] != 0 || vec[1] != 0 {
		t.Errorf("zero vector should remain zero after normalize, got %v", vec)
	}
}

func TestL2Normalize_alreadyUnit(t *testing.T) {
	vec := []float32{1.0}
	l2Normalize(vec)
	if math.Abs(float64(vec[0])-1.0) > 1e-6 {
		t.Errorf("unit vector should stay 1.0, got %v", vec[0])
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch: pure-logic branches (no HTTP)
// ---------------------------------------------------------------------------

func TestEmbedBatch_emptyInput(t *testing.T) {
	e := NewGeminiEmbedder("key", nil)
	result, err := e.EmbedBatch(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty input, got %v", result)
	}
}
