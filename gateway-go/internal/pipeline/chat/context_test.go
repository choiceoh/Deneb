package chat

import (
	"log/slog"
	"testing"
)

func TestDefaultContextConfig(t *testing.T) {
	cfg := DefaultContextConfig()
	if cfg.MemoryTokenBudget != defaultMemoryTokenBudget {
		t.Errorf("MemoryTokenBudget = %d, want %d", cfg.MemoryTokenBudget, defaultMemoryTokenBudget)
	}
	if cfg.FreshTailCount != 48 {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, 48)
	}
}

func TestEstimateTokens(t *testing.T) {
	// estimateTokens now delegates to tokenest.Estimate (script-aware).
	// Verify basic properties rather than exact values, since the
	// estimation engine uses per-script calibration constants.
	t.Run("empty returns zero", func(t *testing.T) {
		if got := estimateTokens(""); got != 0 {
			t.Errorf("estimateTokens(\"\") = %d, want 0", got)
		}
	})
	t.Run("single char returns at least 1", func(t *testing.T) {
		if got := estimateTokens("a"); got < 1 {
			t.Errorf("estimateTokens(\"a\") = %d, want >= 1", got)
		}
	})
	t.Run("longer text returns more tokens", func(t *testing.T) {
		short := estimateTokens("hello")
		long := estimateTokens("hello world this is a longer sentence")
		if long <= short {
			t.Errorf("longer text (%d) should produce more tokens than shorter (%d)", long, short)
		}
	})
	t.Run("Korean text produces reasonable estimate", func(t *testing.T) {
		got := estimateTokens("비금도 해상태양광 프로젝트 현황 보고서를 작성해 주세요")
		// 27 Hangul + 5 spaces → should be in reasonable range.
		if got < 10 || got > 40 {
			t.Errorf("Korean estimate = %d, want 10-40", got)
		}
	})
}

func TestAssembleContextRequiresBridge(t *testing.T) {
	// assembleContext must reject non-Bridge stores.
	store := &nonBridgeStore{}
	cfg := DefaultContextConfig()
	_, err := assembleContext(store, "test", cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for non-Bridge store")
	}
}

// nonBridgeStore is a minimal TranscriptStore that is NOT a polaris.Bridge.
type nonBridgeStore struct{}

func (s *nonBridgeStore) Load(string, int) ([]ChatMessage, int, error) { return nil, 0, nil }
func (s *nonBridgeStore) Append(string, ChatMessage) error             { return nil }
func (s *nonBridgeStore) Delete(string) error                          { return nil }
func (s *nonBridgeStore) ListKeys() ([]string, error)                  { return nil, nil }
func (s *nonBridgeStore) Search(string, int) ([]SearchResult, error)   { return nil, nil }
func (s *nonBridgeStore) CloneRecent(string, string, int) error        { return nil }
