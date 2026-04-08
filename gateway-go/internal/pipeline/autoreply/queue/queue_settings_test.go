package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestResolveFollowupQueueSettings_AlwaysCollect(t *testing.T) {
	// Mode is always collect regardless of input.
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{})
	if s.Mode != types.FollowupModeCollect {
		t.Errorf("got %q, want mode collect", s.Mode)
	}
}

func TestResolveFollowupQueueSettings_Defaults(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{})

	if s.DebounceMs != DefaultFollowupDebounceMs {
		t.Errorf("got %d, want default debounce %d", s.DebounceMs, DefaultFollowupDebounceMs)
	}
	if s.Cap != DefaultFollowupCap {
		t.Errorf("got %d, want default cap %d", s.Cap, DefaultFollowupCap)
	}
	if s.DropPolicy != DefaultFollowupDrop {
		t.Errorf("got %q, want default drop %q", s.DropPolicy, DefaultFollowupDrop)
	}
}

func TestResolveFollowupQueueSettings_CustomValues(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		DebounceMs: 5000,
		Cap:        50,
	})

	if s.DebounceMs != 5000 {
		t.Errorf("got %d, want debounce 5000", s.DebounceMs)
	}
	if s.Cap != 50 {
		t.Errorf("got %d, want cap 50", s.Cap)
	}
	// Drop policy is always summarize.
	if s.DropPolicy != types.FollowupDropSummarize {
		t.Errorf("got %q, want drop summarize", s.DropPolicy)
	}
}
