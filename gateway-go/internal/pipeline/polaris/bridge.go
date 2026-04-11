package polaris

import (
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// Compile-time interface compliance.
var _ toolctx.TranscriptStore = (*Bridge)(nil)

// Bridge wraps an existing TranscriptStore and dual-writes to the Polaris Store.
// It implements the TranscriptStore interface as a drop-in replacement.
//
// On first Load for a session, the Bridge lazily migrates existing JSONL
// messages into the Polaris store (idempotent). Subsequent Appends dual-write
// to both stores.
type Bridge struct {
	legacy   toolctx.TranscriptStore
	store    *Store
	engine   *Engine
	logger   *slog.Logger
	migrated sync.Map // session_key → true
}

// NewBridge wraps a legacy TranscriptStore with Polaris dual-write.
// Creates a long-lived Engine with circuit breaker for the lifecycle of the Bridge.
func NewBridge(legacy toolctx.TranscriptStore, store *Store, logger *slog.Logger) *Bridge {
	return &Bridge{
		legacy: legacy,
		store:  store,
		engine: NewEngine(store, logger, DefaultConfig()),
		logger: logger,
	}
}

// Store returns the underlying Polaris store.
func (b *Bridge) Store() *Store { return b.store }

// Engine returns the long-lived Polaris engine (shared across runs).
func (b *Bridge) Engine() *Engine { return b.engine }

// Load delegates to the legacy store and triggers lazy migration.
func (b *Bridge) Load(sessionKey string, limit int) ([]toolctx.ChatMessage, int, error) {
	b.ensureMigrated(sessionKey)
	return b.legacy.Load(sessionKey, limit)
}

// Append writes to both legacy JSONL and Polaris file store.
func (b *Bridge) Append(sessionKey string, msg toolctx.ChatMessage) error {
	if err := b.legacy.Append(sessionKey, msg); err != nil {
		return err
	}
	if err := b.store.AppendMessage(sessionKey, msg); err != nil {
		b.logger.Warn("polaris: dual-write failed",
			"session", sessionKey, "error", err)
	}
	return nil
}

// Delete removes from both stores.
func (b *Bridge) Delete(sessionKey string) error {
	if err := b.legacy.Delete(sessionKey); err != nil {
		return err
	}
	b.migrated.Delete(sessionKey)
	if err := b.store.DeleteSession(sessionKey); err != nil {
		b.logger.Warn("polaris: delete failed", "session", sessionKey, "error", err)
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

// AssembleContext builds the LLM context for a session via the Polaris summary DAG.
// It ensures legacy JSONL data is migrated first, then delegates to the DAG-based
// assembly which uses summaries for old messages and raw messages for recent ones.
func (b *Bridge) AssembleContext(
	sessionKey string,
	memoryTokenBudget int,
	freshTailCount int,
	logger *slog.Logger,
) (*AssemblyResult, error) {
	b.ensureMigrated(sessionKey)
	return assembleContextFull(b.store, sessionKey, memoryTokenBudget, freshTailCount, logger)
}

// AssemblyResult holds the output of context assembly.
type AssemblyResult struct {
	Messages        []llm.Message
	EstimatedTokens int
	TotalMessages   int
	WasCompacted    bool // true if summary nodes were used
}

// ensureMigrated runs lazy migration for a session (once per process lifetime).
func (b *Bridge) ensureMigrated(sessionKey string) {
	if _, loaded := b.migrated.LoadOrStore(sessionKey, true); loaded {
		return
	}
	if err := MigrateSession(b.legacy, b.store, sessionKey, b.logger); err != nil {
		b.logger.Warn("polaris: lazy migration failed", "session", sessionKey, "error", err)
	}
}
