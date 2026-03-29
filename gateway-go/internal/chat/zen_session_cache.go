// session_cache.go — L3 session-scoped cache for compiled prompts and context snapshots.
//
// CPU architecture analogy: multi-level cache hierarchy. TurnContext is L1 (per-turn),
// RunCache is L2 (per-run), and SessionCache is L3 (per-session, survives across runs).
//
// Caches:
//   - Compiled system prompt (string + Anthropic blocks) — avoids rebuilding static sections
//     that don't change between runs in the same session (~10-50ms saved per run).
//   - Context assembly snapshots — avoids full transcript re-traversal when the message
//     count hasn't changed (50-200ms saved per run).
//
// Invalidation:
//   - System prompt: invalidated when tool definitions change or context files are modified
//     (detected by tool count + workspace dir hash).
//   - Context snapshots: invalidated when transcript message count changes (write-through
//     cache ensures consistency).
//   - TTL: 60 seconds for prompts, 30 seconds for context snapshots.
//
// Thread-safe for single-user deployment (sync.Mutex is sufficient).
package chat

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SessionCache is an L3 cache scoped to a gateway process lifetime.
// Single-user deployment: one process, one cache, no cross-process coordination.
type SessionCache struct {
	mu       sync.Mutex
	prompts  map[string]*promptCacheEntry  // key: cacheKeyForPrompt(channel, toolCount, workspaceDir)
	contexts map[string]*contextCacheEntry // key: sessionKey
}

// promptCacheEntry caches a compiled system prompt.
type promptCacheEntry struct {
	prompt    json.RawMessage // compiled system prompt (string or blocks format)
	expiresAt time.Time
}

// contextCacheEntry caches an assembled context window.
type contextCacheEntry struct {
	messages     []llm.Message
	messageCount int // transcript total at cache time — used for invalidation
	expiresAt    time.Time
}

const (
	promptCacheTTL  = 60 * time.Second
	contextCacheTTL = 30 * time.Second
)

// NewSessionCache creates an empty L3 session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{
		prompts:  make(map[string]*promptCacheEntry),
		contexts: make(map[string]*contextCacheEntry),
	}
}

// GetPrompt returns a cached system prompt if valid.
func (sc *SessionCache) GetPrompt(key string) (json.RawMessage, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	entry, ok := sc.prompts[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(sc.prompts, key)
		}
		return nil, false
	}
	return entry.prompt, true
}

// SetPrompt stores a compiled system prompt.
func (sc *SessionCache) SetPrompt(key string, prompt json.RawMessage) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.prompts[key] = &promptCacheEntry{
		prompt:    prompt,
		expiresAt: time.Now().Add(promptCacheTTL),
	}
}

// GetContext returns a cached context assembly if the message count matches.
// A mismatch means new messages have been added since caching — the snapshot
// is stale and must be reassembled.
func (sc *SessionCache) GetContext(sessionKey string, currentMsgCount int) ([]llm.Message, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	entry, ok := sc.contexts[sessionKey]
	if !ok || time.Now().After(entry.expiresAt) || entry.messageCount != currentMsgCount {
		if ok {
			delete(sc.contexts, sessionKey)
		}
		return nil, false
	}

	// Return a copy to prevent mutation of cached data.
	copied := make([]llm.Message, len(entry.messages))
	copy(copied, entry.messages)
	return copied, true
}

// SetContext stores an assembled context window.
func (sc *SessionCache) SetContext(sessionKey string, messages []llm.Message, msgCount int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	stored := make([]llm.Message, len(messages))
	copy(stored, messages)
	sc.contexts[sessionKey] = &contextCacheEntry{
		messages:     stored,
		messageCount: msgCount,
		expiresAt:    time.Now().Add(contextCacheTTL),
	}
}

// InvalidateContext removes a cached context for a session.
// Called when the transcript changes (e.g., after compaction).
func (sc *SessionCache) InvalidateContext(sessionKey string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.contexts, sessionKey)
}

// InvalidateAllPrompts clears all cached prompts.
// Called when tool definitions or context files change.
func (sc *SessionCache) InvalidateAllPrompts() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.prompts = make(map[string]*promptCacheEntry)
}

// PromptCacheKey builds a cache key for compiled system prompts.
// Key components: channel type, tool count, workspace dir, and API type.
// These are the inputs that determine the prompt content — if any change,
// the cache must be invalidated.
func PromptCacheKey(channel string, toolCount int, workspaceDir string, apiType string) string {
	return fmt.Sprintf("%s|%s|%s|%d", channel, apiType, workspaceDir, toolCount)
}
