package chat

import (
	"testing"
)

func TestDefaultCompactionConfig(t *testing.T) {
	cfg := DefaultCompactionConfig()
	if cfg.ContextThreshold != 0.80 {
		t.Errorf("ContextThreshold = %f, want %f", cfg.ContextThreshold, 0.80)
	}
	if cfg.FreshTailCount != defaultFreshTailCount {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, defaultFreshTailCount)
	}
}
