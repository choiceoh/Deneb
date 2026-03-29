// queue_enqueue.go — Followup queue enqueue logic with deduplication.
// Mirrors src/auto-reply/reply/queue/enqueue.ts (118 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"strings"
	"sync"
	"time"
)

const (
	// recentMessageIDTTL is the TTL for the recent message ID dedup cache.
	recentMessageIDTTL = 5 * time.Minute
	// recentMessageIDMaxSize is the max entries in the dedup cache.
	recentMessageIDMaxSize = 10_000
)

// dedupeEntry tracks a recently seen message ID.
type dedupeEntry struct {
	seenAt time.Time
}

// RecentMessageIDCache is a bounded TTL cache for message ID deduplication.
type RecentMessageIDCache struct {
	mu      sync.Mutex
	entries map[string]dedupeEntry
	checks  int
}

func NewRecentMessageIDCache() *RecentMessageIDCache {
	return &RecentMessageIDCache{
		entries: make(map[string]dedupeEntry),
	}
}

// peek returns true if the key is in the cache and not expired.
func (c *RecentMessageIDCache) peek(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return false
	}
	if time.Since(entry.seenAt) > recentMessageIDTTL {
		delete(c.entries, key)
		return false
	}
	return true
}

// check adds a key to the cache and prunes expired entries if over capacity.
func (c *RecentMessageIDCache) check(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = dedupeEntry{seenAt: time.Now()}
	c.checks++
	if len(c.entries) > recentMessageIDMaxSize || c.checks%100 == 0 {
		c.prune()
	}
}

// clear removes all entries.
func (c *RecentMessageIDCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]dedupeEntry)
	c.checks = 0
}

func (c *RecentMessageIDCache) prune() {
	now := time.Now()
	for k, v := range c.entries {
		if now.Sub(v.seenAt) > recentMessageIDTTL {
			delete(c.entries, k)
		}
	}
}

// buildRecentMessageIDKey builds a dedup key for a followup run.
func buildRecentMessageIDKey(run types.FollowupRun, queueKey string) string {
	messageID := strings.TrimSpace(run.MessageID)
	if messageID == "" {
		return ""
	}
	threadID := run.OriginatingThreadID
	return strings.Join([]string{
		"queue", queueKey,
		run.OriginatingChannel,
		run.OriginatingTo,
		run.OriginatingAccountID,
		threadID,
		messageID,
	}, "|")
}

// isRunAlreadyQueued checks if a run is already in the queue.
func isRunAlreadyQueued(run types.FollowupRun, items []types.FollowupRun, allowPromptFallback bool) bool {
	hasSameRouting := func(item types.FollowupRun) bool {
		return item.OriginatingChannel == run.OriginatingChannel &&
			item.OriginatingTo == run.OriginatingTo &&
			item.OriginatingAccountID == run.OriginatingAccountID &&
			item.OriginatingThreadID == run.OriginatingThreadID
	}

	messageID := strings.TrimSpace(run.MessageID)
	if messageID != "" {
		for _, item := range items {
			if strings.TrimSpace(item.MessageID) == messageID && hasSameRouting(item) {
				return true
			}
		}
		return false
	}
	if !allowPromptFallback {
		return false
	}
	for _, item := range items {
		if item.Prompt == run.Prompt && hasSameRouting(item) {
			return true
		}
	}
	return false
}

// EnqueueFollowupRun adds a run to the followup queue with deduplication.
// Returns true if the run was enqueued, false if it was deduplicated or dropped.
func (r *FollowupQueueRegistry) EnqueueFollowupRun(
	key string,
	run types.FollowupRun,
	settings types.FollowupQueueSettings,
	dedupeMode types.FollowupDedupeMode,
	recentIDs *RecentMessageIDCache,
) bool {
	queue := r.GetOrCreate(key, settings)

	// Check recent message ID cache (lock-free, cache has its own mutex).
	var recentKey string
	if dedupeMode != types.DedupeNone {
		recentKey = buildRecentMessageIDKey(run, key)
		if recentKey != "" && recentIDs.peek(recentKey) {
			return false
		}
	}

	// All queue field access under the per-queue lock.
	queue.Lock()
	defer queue.Unlock()

	// Check in-queue deduplication.
	allowPrompt := dedupeMode == types.DedupePrompt
	if dedupeMode != types.DedupeNone && isRunAlreadyQueued(run, queue.Items, allowPrompt) {
		return false
	}

	queue.LastEnqueuedAt = time.Now().UnixMilli()
	queue.LastRun = run.Run

	// Apply drop policy if at capacity.
	if len(queue.Items) >= queue.Cap {
		switch queue.DropPolicy {
		case types.FollowupDropNew:
			queue.DroppedCount++
			return false
		case types.FollowupDropOld:
			if len(queue.Items) > 0 {
				queue.Items = queue.Items[1:]
			}
			queue.DroppedCount++
		case types.FollowupDropSummarize:
			summary := strings.TrimSpace(run.SummaryLine)
			if summary == "" {
				summary = strings.TrimSpace(run.Prompt)
			}
			if summary != "" {
				queue.SummaryLines = append(queue.SummaryLines, summary)
			}
			queue.DroppedCount++
			return false
		}
	}

	queue.Items = append(queue.Items, run)
	if recentKey != "" {
		recentIDs.check(recentKey)
	}
	return true
}

// ResetRecentQueuedMessageIDDedupe clears the dedup cache.
func (c *RecentMessageIDCache) reset() {
	c.clear()
}
