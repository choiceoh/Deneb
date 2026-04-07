package lcm

import (
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// MigrateSession backfills the LCM store from the legacy JSONL transcript.
// It is idempotent: if the LCM store already has messages for this session,
// only the delta (new messages since last migration) is imported.
//
// Called lazily on first session access via Bridge, not eagerly at startup.
func MigrateSession(legacy toolctx.TranscriptStore, store *Store, sessionKey string, logger *slog.Logger) error {
	lcmCount, err := store.MessageCount(sessionKey)
	if err != nil {
		return fmt.Errorf("lcm migrate: count: %w", err)
	}

	// Load all messages from legacy store (limit=0 means all).
	legacyMsgs, legacyTotal, err := legacy.Load(sessionKey, 0)
	if err != nil {
		return fmt.Errorf("lcm migrate: load legacy: %w", err)
	}

	if lcmCount >= legacyTotal {
		return nil // already up to date
	}

	// Import only the delta.
	delta := legacyMsgs[lcmCount:]
	for _, msg := range delta {
		if err := store.AppendMessage(sessionKey, msg); err != nil {
			return fmt.Errorf("lcm migrate: append at offset %d: %w", lcmCount, err)
		}
		lcmCount++
	}

	logger.Info("lcm: migrated session",
		"session", sessionKey,
		"imported", len(delta),
		"total", legacyTotal)
	return nil
}
