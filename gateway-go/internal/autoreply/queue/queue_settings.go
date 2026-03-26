// queue_settings.go — Followup queue settings resolution.
// Mirrors src/auto-reply/reply/queue/settings.ts (72 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// ResolveFollowupQueueSettings resolves the effective queue settings from config,
// session entry, inline overrides, and per-channel defaults.
func ResolveFollowupQueueSettings(params types.ResolveFollowupQueueSettingsParams) types.FollowupQueueSettings {
	// Resolve mode: inline > session > config > channel default.
	mode := params.InlineMode
	if mode == "" {
		mode = NormalizeFollowupQueueMode(params.SessionMode)
	}
	if mode == "" {
		mode = NormalizeFollowupQueueMode(params.ConfigMode)
	}
	if mode == "" {
		mode = defaultFollowupQueueModeForChannel(params.Channel)
	}

	// Resolve debounce.
	debounce := params.DebounceMs
	if debounce <= 0 {
		debounce = DefaultFollowupDebounceMs
	}

	// Resolve cap.
	cap := params.Cap
	if cap <= 0 {
		cap = DefaultFollowupCap
	}

	// Resolve drop policy.
	drop := params.DropPolicy
	if drop == "" {
		drop = DefaultFollowupDrop
	}

	return types.FollowupQueueSettings{
		Mode:       mode,
		DebounceMs: debounce,
		Cap:        cap,
		DropPolicy: drop,
	}
}

// defaultFollowupQueueModeForChannel returns the default queue mode per channel.
func defaultFollowupQueueModeForChannel(_ string) types.FollowupQueueMode {
	return types.FollowupModeCollect
}
