// queue_settings.go — Followup queue settings resolution.
// Mirrors src/auto-reply/reply/queue/settings.ts (72 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// ResolveFollowupQueueSettings resolves the effective queue settings.
// The queue always operates in collect (auto-debounce) mode for the
// single-user Telegram bot. Debounce/cap can still be overridden.
func ResolveFollowupQueueSettings(params types.ResolveFollowupQueueSettingsParams) types.FollowupQueueSettings {
	// Always collect mode (auto-debounce).
	mode := types.FollowupModeCollect

	// Resolve debounce.
	debounce := params.DebounceMs
	if debounce <= 0 {
		debounce = DefaultFollowupDebounceMs
	}

	// Resolve cap.
	queueCap := params.Cap
	if queueCap <= 0 {
		queueCap = DefaultFollowupCap
	}

	return types.FollowupQueueSettings{
		Mode:       mode,
		DebounceMs: debounce,
		Cap:        queueCap,
		DropPolicy: DefaultFollowupDrop,
	}
}
