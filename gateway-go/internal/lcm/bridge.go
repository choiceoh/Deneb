package lcm

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Bridge wraps an existing TranscriptStore and dual-writes to the LCM Store.
// It implements the TranscriptStore interface as a drop-in replacement.
//
// Read operations (Load, Search, CloneRecent) are delegated to the legacy store.
// Write operations (Append) write to both legacy JSONL and LCM SQLite.
// This allows incremental adoption: Phase 1 accumulates data without changing
// read behavior; Phase 3 will switch reads to LCM-aware context assembly.
type Bridge struct {
	legacy toolctx.TranscriptStore
	store  *Store
	logger *slog.Logger
}

// NewBridge wraps a legacy TranscriptStore with LCM dual-write.
func NewBridge(legacy toolctx.TranscriptStore, store *Store, logger *slog.Logger) *Bridge {
	return &Bridge{
		legacy: legacy,
		store:  store,
		logger: logger,
	}
}

// Store returns the underlying LCM store (for direct queries by engine/tools).
func (b *Bridge) Store() *Store { return b.store }

// Load delegates to the legacy store.
func (b *Bridge) Load(sessionKey string, limit int) ([]toolctx.ChatMessage, int, error) {
	return b.legacy.Load(sessionKey, limit)
}

// Append writes to both legacy JSONL and LCM SQLite.
// Legacy write is authoritative; LCM failure is logged but does not fail the call.
func (b *Bridge) Append(sessionKey string, msg toolctx.ChatMessage) error {
	if err := b.legacy.Append(sessionKey, msg); err != nil {
		return err
	}
	if err := b.store.AppendMessage(sessionKey, msg); err != nil {
		b.logger.Warn("lcm: dual-write failed, message saved to JSONL only",
			"session", sessionKey, "error", err)
	}
	return nil
}

// Delete removes from both stores.
func (b *Bridge) Delete(sessionKey string) error {
	if err := b.legacy.Delete(sessionKey); err != nil {
		return err
	}
	if err := b.store.DeleteSession(sessionKey); err != nil {
		b.logger.Warn("lcm: delete from sqlite failed", "session", sessionKey, "error", err)
	}
	return nil
}

// ListKeys delegates to the legacy store.
func (b *Bridge) ListKeys() ([]string, error) {
	return b.legacy.ListKeys()
}

// Search delegates to the legacy store.
func (b *Bridge) Search(query string, maxResults int) ([]toolctx.SearchResult, error) {
	return b.legacy.Search(query, maxResults)
}

// CloneRecent delegates to the legacy store.
func (b *Bridge) CloneRecent(srcKey, dstKey string, limit int) error {
	return b.legacy.CloneRecent(srcKey, dstKey, limit)
}
