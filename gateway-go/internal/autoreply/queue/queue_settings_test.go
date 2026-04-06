package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"testing"
)

func TestResolveFollowupQueueSettings_AlwaysCollect(t *testing.T) {
	// Mode is always collect regardless of input.
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{})
	if s.Mode != types.FollowupModeCollect {
		t.Errorf("expected mode collect, got %q", s.Mode)
	}
}

func TestResolveFollowupQueueSettings_Defaults(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{})

	if s.DebounceMs != DefaultFollowupDebounceMs {
		t.Errorf("expected default debounce %d, got %d", DefaultFollowupDebounceMs, s.DebounceMs)
	}
	if s.Cap != DefaultFollowupCap {
		t.Errorf("expected default cap %d, got %d", DefaultFollowupCap, s.Cap)
	}
	if s.DropPolicy != DefaultFollowupDrop {
		t.Errorf("expected default drop %q, got %q", DefaultFollowupDrop, s.DropPolicy)
	}
}

func TestResolveFollowupQueueSettings_CustomValues(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		DebounceMs: 5000,
		Cap:        50,
	})

	if s.DebounceMs != 5000 {
		t.Errorf("expected debounce 5000, got %d", s.DebounceMs)
	}
	if s.Cap != 50 {
		t.Errorf("expected cap 50, got %d", s.Cap)
	}
	// Drop policy is always summarize.
	if s.DropPolicy != types.FollowupDropSummarize {
		t.Errorf("expected drop summarize, got %q", s.DropPolicy)
	}
}
