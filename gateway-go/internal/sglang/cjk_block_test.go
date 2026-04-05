package sglang

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLoadCJKBlock(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "cjk_tokens.json")

	data := cjkTokenFile{
		Model:           "test-model",
		VocabSize:       100000,
		BlockedCount:    3,
		KoreanCount:     500,
		MixedCount:      2,
		BlockedTokenIDs: []int{1234, 5678, 9012},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(tokenFile, raw, 0644)

	t.Run("loads valid file", func(t *testing.T) {
		b := loadCJKBlock(tokenFile, testLogger())
		if b == nil {
			t.Fatal("expected non-nil cjkBlock")
		}
		if b.count != 3 {
			t.Errorf("expected count=3, got %d", b.count)
		}
		// Verify pre-serialized JSON is valid and contains expected entries.
		var bias map[string]float64
		if err := json.Unmarshal(b.raw, &bias); err != nil {
			t.Fatalf("pre-serialized JSON invalid: %v", err)
		}
		if len(bias) != 3 {
			t.Errorf("expected 3 entries, got %d", len(bias))
		}
		for _, key := range []string{"1234", "5678", "9012"} {
			if v, ok := bias[key]; !ok || v != -100 {
				t.Errorf("expected %s=-100, got %v (ok=%v)", key, v, ok)
			}
		}
	})

	t.Run("empty path returns nil", func(t *testing.T) {
		b := loadCJKBlock("", testLogger())
		if b != nil {
			t.Error("expected nil for empty path")
		}
	})

	t.Run("missing file returns nil", func(t *testing.T) {
		b := loadCJKBlock("/nonexistent/path.json", testLogger())
		if b != nil {
			t.Error("expected nil for missing file")
		}
	})

	t.Run("empty token list returns nil", func(t *testing.T) {
		emptyFile := filepath.Join(dir, "empty.json")
		empty := cjkTokenFile{BlockedTokenIDs: []int{}}
		raw, _ := json.Marshal(empty)
		os.WriteFile(emptyFile, raw, 0644)

		b := loadCJKBlock(emptyFile, testLogger())
		if b != nil {
			t.Error("expected nil for empty token list")
		}
	})
}

func TestCJKBlock_MergeInto(t *testing.T) {
	bias := map[string]float64{"100": -100, "200": -100}
	raw, _ := json.Marshal(bias)
	b := &cjkBlock{raw: json.RawMessage(raw), count: 2}

	t.Run("injects pre-serialized bytes", func(t *testing.T) {
		extra := map[string]any{"timeout": 30.0}
		b.mergeInto(extra)

		v, ok := extra["logit_bias"]
		if !ok {
			t.Fatal("logit_bias not injected")
		}
		// Should be json.RawMessage (not re-marshaled).
		rm, ok := v.(json.RawMessage)
		if !ok {
			t.Fatalf("expected json.RawMessage, got %T", v)
		}
		// Verify it round-trips correctly.
		var decoded map[string]float64
		if err := json.Unmarshal(rm, &decoded); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if decoded["100"] != -100 || decoded["200"] != -100 {
			t.Errorf("unexpected values: %v", decoded)
		}
	})

	t.Run("nil block is no-op", func(t *testing.T) {
		var nilBlock *cjkBlock
		extra := map[string]any{"foo": "bar"}
		nilBlock.mergeInto(extra)
		if _, ok := extra["logit_bias"]; ok {
			t.Error("nil block should not inject logit_bias")
		}
	})
}

func TestCJKBlock_ConditionalInjection(t *testing.T) {
	bias := map[string]float64{"100": -100}
	raw, _ := json.Marshal(bias)
	b := &cjkBlock{raw: json.RawMessage(raw), count: 1}

	t.Run("default ApplyCJKBlock=false skips injection", func(t *testing.T) {
		req := &Request{CallerTag: "memory_json"}
		merged := map[string]any{"timeout": 30.0}

		// Simulate executeRequest logic: only inject when ApplyCJKBlock is true.
		if req.ApplyCJKBlock {
			if _, hasGuided := merged["guided_json"]; !hasGuided {
				b.mergeInto(merged)
			}
		}

		if _, ok := merged["logit_bias"]; ok {
			t.Error("CJK block should NOT be injected when ApplyCJKBlock is false")
		}
	})

	t.Run("ApplyCJKBlock=true injects", func(t *testing.T) {
		req := &Request{CallerTag: "calllocal", ApplyCJKBlock: true}
		merged := map[string]any{"timeout": 30.0}

		if req.ApplyCJKBlock {
			if _, hasGuided := merged["guided_json"]; !hasGuided {
				b.mergeInto(merged)
			}
		}

		if _, ok := merged["logit_bias"]; !ok {
			t.Error("CJK block should be injected when ApplyCJKBlock is true")
		}
	})

	t.Run("ApplyCJKBlock=true with guided_json skips injection", func(t *testing.T) {
		req := &Request{CallerTag: "test", ApplyCJKBlock: true}
		merged := map[string]any{
			"guided_json": json.RawMessage(`{"type":"object"}`),
		}

		if req.ApplyCJKBlock {
			if _, hasGuided := merged["guided_json"]; !hasGuided {
				b.mergeInto(merged)
			}
		}

		if _, ok := merged["logit_bias"]; ok {
			t.Error("CJK block should NOT be injected when guided_json is present")
		}
	})
}

func TestCJKBlock_MarshalPassthrough(t *testing.T) {
	// Verify that json.Marshal on json.RawMessage doesn't re-encode.
	bias := map[string]float64{"12345": -100}
	raw, _ := json.Marshal(bias)
	rm := json.RawMessage(raw)

	// When mergeJSONFields calls json.Marshal(v) on a json.RawMessage,
	// it should return the bytes as-is.
	out, err := json.Marshal(rm)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("expected passthrough:\n  got:  %s\n  want: %s", out, raw)
	}
}
