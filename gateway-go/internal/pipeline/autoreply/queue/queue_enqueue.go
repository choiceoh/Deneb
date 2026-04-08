// queue_enqueue.go — Followup queue enqueue logic with deduplication.
// Mirrors src/auto-reply/reply/queue/enqueue.ts (118 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"strings"
	"time"
)

const (
	// recentMessageIDMaxSize is the max entries before the dedup cache resets.
	// Single-user deployment: 100 entries is plenty.
	recentMessageIDMaxSize = 100
)

// RecentMessageIDCache is a simple bounded set for message ID deduplication.
// Single-user deployment — no TTL, no FIFO queue, no mutex needed.
// When the map exceeds recentMessageIDMaxSize, it clears entirely.
type RecentMessageIDCache struct {
	entries map[string]struct{}
}

func NewRecentMessageIDCache() *RecentMessageIDCache {
	return &RecentMessageIDCache{
		entries: make(map[string]struct{}),
	}
}

// peek returns true if the key is in the cache.
func (c *RecentMessageIDCache) peek(key string) bool {
	_, ok := c.entries[key]
	return ok
}

// check adds a key to the cache. Clears the map if over capacity.
func (c *RecentMessageIDCache) check(key string) {
	if len(c.entries) >= recentMessageIDMaxSize {
		c.entries = make(map[string]struct{})
	}
	c.entries[key] = struct{}{}
}

// clear removes all entries.
func (c *RecentMessageIDCache) clear() {
	c.entries = make(map[string]struct{})
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
	queue.mu.Lock()
	defer queue.mu.Unlock()

	// Check in-queue deduplication.
	allowPrompt := dedupeMode == types.DedupePrompt
	if dedupeMode != types.DedupeNone && isRunAlreadyQueued(run, queue.Items, allowPrompt) {
		return false
	}

	queue.LastEnqueuedAt = time.Now().UnixMilli()
	queue.LastRun = run.Run

	// At capacity: summarize and drop (single-user bot, always summarize policy).
	if len(queue.Items) >= queue.Cap {
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

	queue.Items = append(queue.Items, run)
	if recentKey != "" {
		recentIDs.check(recentKey)
	}
	return true
}
