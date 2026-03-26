package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"testing"
)

func TestResolveFollowupQueueSettings_InlinePriority(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		InlineMode:  types.FollowupModeSteer,
		SessionMode: "collect",
		ConfigMode:  "followup",
	})
	if s.Mode != types.FollowupModeSteer {
		t.Errorf("expected inline mode steer, got %q", s.Mode)
	}
}

func TestResolveFollowupQueueSettings_SessionFallback(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		SessionMode: "collect",
		ConfigMode:  "followup",
	})
	if s.Mode != types.FollowupModeCollect {
		t.Errorf("expected session mode collect, got %q", s.Mode)
	}
}

func TestResolveFollowupQueueSettings_ConfigFallback(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		ConfigMode: "followup",
	})
	if s.Mode != types.FollowupModeFollowup {
		t.Errorf("expected config mode followup, got %q", s.Mode)
	}
}

func TestResolveFollowupQueueSettings_ChannelDefault(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		Channel: "telegram",
	})
	// Default for any channel is collect.
	if s.Mode != types.FollowupModeCollect {
		t.Errorf("expected default mode collect, got %q", s.Mode)
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
		InlineMode: types.FollowupModeInterrupt,
		DebounceMs: 5000,
		Cap:        50,
		DropPolicy: types.FollowupDropNew,
	})

	if s.DebounceMs != 5000 {
		t.Errorf("expected debounce 5000, got %d", s.DebounceMs)
	}
	if s.Cap != 50 {
		t.Errorf("expected cap 50, got %d", s.Cap)
	}
	if s.DropPolicy != types.FollowupDropNew {
		t.Errorf("expected drop new, got %q", s.DropPolicy)
	}
}
