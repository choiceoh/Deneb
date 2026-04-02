package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
)

func TestDefaultSweepConfig(t *testing.T) {
	cfg := aurora.DefaultSweepConfig()
	if cfg.ContextThreshold != 0.80 {
		t.Errorf("ContextThreshold = %f, want %f", cfg.ContextThreshold, 0.80)
	}
	if cfg.FreshTailCount != 8 {
		t.Errorf("FreshTailCount = %d, want %d", cfg.FreshTailCount, 8)
	}
}
