package polaris

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Compile-time interface compliance.
var (
	_ chatport.TranscriptStore                = (*Bridge)(nil)
	_ chatport.SemanticSessionSearcher        = (*Bridge)(nil)
	_ chatport.ToolResultReceiptStoreProvider = (*Bridge)(nil)
)

// Bridge wraps an existing TranscriptStore and dual-writes to the Polaris Store.
// It implements the TranscriptStore interface as a drop-in replacement.
//
// On first Load for a session, the Bridge lazily migrates existing JSONL
// messages into the Polaris store (idempotent). Subsequent Appends dual-write
// to both stores.
type Bridge struct {
	legacy            chatport.TranscriptStore
	store             *Store
	engine            *Engine
	logger            *slog.Logger
	strictPersistence bool
	migrated          sync.Map // session_key → true
	strictMigrationMu sync.Mutex
}

// BridgeOptions controls optional persistence behavior. The zero value keeps
// production's historical best-effort Polaris mirror: the legacy transcript
// remains authoritative and Polaris failures are logged.
type BridgeOptions struct {
	// StrictPersistence makes every Polaris migration, append, and delete
	// failure observable to the caller. It is intended for isolated evaluators
	// such as Deneb-Briefcase, where continuing after a partial write would make
	// the evidence bundle non-reproducible.
	StrictPersistence bool
}

// NewBridge wraps a legacy TranscriptStore with Polaris dual-write.
// Creates a long-lived Engine with circuit breaker for the lifecycle of the Bridge.
func NewBridge(legacy chatport.TranscriptStore, store *Store, logger *slog.Logger) *Bridge {
	return NewBridgeWithOptions(legacy, store, logger, BridgeOptions{})
}

// NewBridgeWithOptions wraps a legacy TranscriptStore with explicit
// persistence behavior. Production callers should normally use NewBridge;
// isolated evaluators can opt into fail-closed persistence.
func NewBridgeWithOptions(
	legacy chatport.TranscriptStore,
	store *Store,
	logger *slog.Logger,
	options BridgeOptions,
) *Bridge {
	return &Bridge{
		legacy:            legacy,
		store:             store,
		engine:            NewEngine(store, logger, DefaultConfig()),
		logger:            logger,
		strictPersistence: options.StrictPersistence,
	}
}

// Store returns the underlying Polaris store.
func (b *Bridge) Store() *Store { return b.store }

// Engine returns the long-lived Polaris engine (shared across runs).
func (b *Bridge) Engine() *Engine { return b.engine }

// Load delegates to the legacy store and triggers lazy migration.
func (b *Bridge) Load(sessionKey string, limit int) ([]chatport.ChatMessage, int, error) {
	if err := b.ensureMigrated(sessionKey); err != nil {
		return nil, 0, err
	}
	return b.legacy.Load(sessionKey, limit)
}

// Append writes to both legacy JSONL and Polaris file store.
func (b *Bridge) Append(sessionKey string, msg chatport.ChatMessage) error {
	// A strict bridge must establish the legacy prefix before it dual-writes a
	// new tail message. Otherwise a fresh Bridge over an existing transcript
	// could place the new message at Polaris index zero and make the subsequent
	// delta migration skip the actual first legacy message.
	if b.strictPersistence {
		if err := b.ensureMigrated(sessionKey); err != nil {
			return err
		}
	}
	if err := b.legacy.Append(sessionKey, msg); err != nil {
		return err
	}
	if err := b.store.AppendMessage(sessionKey, msg); err != nil {
		b.logger.Warn("polaris: dual-write failed",
			"session", sessionKey, "error", err)
		if b.strictPersistence {
			return fmt.Errorf("polaris: strict dual-write append failed for session %q: %w", sessionKey, err)
		}
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
		if b.strictPersistence {
			return fmt.Errorf("polaris: strict delete failed for session %q: %w", sessionKey, err)
		}
	}
	return nil
}

// ToolResultReceiptStore exposes the legacy JSONL store's optional
// crash-recovery journal. Polaris mirrors canonical transcript messages only;
// ephemeral receipts remain beside the legacy transcript.
func (b *Bridge) ToolResultReceiptStore() chatport.ToolResultReceiptStore {
	return chatport.ResolveToolResultReceiptStore(b.legacy)
}

// ListKeys delegates to the legacy store.
func (b *Bridge) ListKeys() ([]string, error) {
	return b.legacy.ListKeys()
}

// Search delegates to the legacy store.
func (b *Bridge) Search(query string, maxResults int) ([]chatport.SearchResult, error) {
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
	if err := b.ensureMigrated(sessionKey); err != nil {
		return nil, err
	}
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
//
// The default path deliberately retains production's existing best-effort
// behavior. Strict bridges serialize migration so no caller can observe a
// session as migrated before the copy has completed, and record success only
// after every message is durable in Polaris.
func (b *Bridge) ensureMigrated(sessionKey string) error {
	if b.strictPersistence {
		b.strictMigrationMu.Lock()
		defer b.strictMigrationMu.Unlock()
		if _, migrated := b.migrated.Load(sessionKey); migrated {
			return nil
		}
		if err := MigrateSession(b.legacy, b.store, sessionKey, b.logger); err != nil {
			b.logger.Warn("polaris: lazy migration failed", "session", sessionKey, "error", err)
			return fmt.Errorf("polaris: strict lazy migration failed for session %q: %w", sessionKey, err)
		}
		b.migrated.Store(sessionKey, true)
		return nil
	}

	if _, loaded := b.migrated.LoadOrStore(sessionKey, true); loaded {
		return nil
	}
	if err := MigrateSession(b.legacy, b.store, sessionKey, b.logger); err != nil {
		b.logger.Warn("polaris: lazy migration failed", "session", sessionKey, "error", err)
	}
	return nil
}

// SearchSessionsSemantic implements chatport.SemanticSessionSearcher: a
// meaning-level search over resident sessions' summary embeddings, the same
// index the recall preflight's summary arm uses (SearchSummariesSemantic).
// Exposing it through the sessions tool lets the model enumerate PAST
// CONVERSATIONS BY CONCEPT when keyword search cannot — "병원 몇 번 갔지"
// whose instances say 피부과/이비인후과 and share no keyword. nil (degrade to
// keyword-only) when the semantic index is disabled.
func (b *Bridge) SearchSessionsSemantic(ctx context.Context, excludeKey, query string, limit int) []chatport.SemanticSessionHit {
	if b == nil || b.store == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	batch := b.store.SearchSummariesSemantic(ctx, excludeKey, []string{query}, limit)
	if len(batch) == 0 || len(batch[0]) == 0 {
		return nil
	}
	hits := batch[0]
	out := make([]chatport.SemanticSessionHit, 0, len(hits))
	seen := make(map[string]struct{}, len(hits))
	for _, h := range hits {
		// One line per session: several summary nodes of the same conversation
		// would otherwise crowd the list the model reads.
		if _, dup := seen[h.SessionKey]; dup {
			continue
		}
		seen[h.SessionKey] = struct{}{}
		out = append(out, chatport.SemanticSessionHit{
			SessionKey: h.SessionKey,
			Snippet:    textutil.TruncateRunes(h.Content, 200, "..."),
			At:         h.CreatedAt,
			Score:      h.Score,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}
