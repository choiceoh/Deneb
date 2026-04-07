// queue_enqueue.go — Followup queue enqueue logic with deduplication.
// Mirrors src/auto-reply/reply/queue/enqueue.ts (118 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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

// expiryEntry is a node in the FIFO expiry queue.
type expiryEntry struct {
	key       string
	expiresAt time.Time
}

// RecentMessageIDCache is a bounded TTL cache for message ID deduplication.
//
// Expiry design: instead of scanning the whole map every N operations, we
// maintain a FIFO expiry queue (entries are appended in insertion order and
// therefore roughly sorted by expiration time). prune() sweeps from the front
// of the queue until it hits a non-expired entry, giving O(1) amortized cost
// per prune call regardless of map size.
//
// A key may appear more than once in the queue if it was re-added after its
// initial insertion; the stale queue entry is harmlessly skipped during prune
// because we verify the map entry's actual seenAt before deleting.
type RecentMessageIDCache struct {
	mu      sync.Mutex
	entries map[string]dedupeEntry
	expiry  []expiryEntry // FIFO; front holds the earliest expiration
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
	now := time.Now()
	c.entries[key] = dedupeEntry{seenAt: now}
	c.expiry = append(c.expiry, expiryEntry{key: key, expiresAt: now.Add(recentMessageIDTTL)})
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
	c.expiry = c.expiry[:0]
	c.checks = 0
}

// prune sweeps expired entries from the front of the FIFO expiry queue.
// Because entries are appended in time order the front always holds the
// oldest (soonest-to-expire) entries, so we stop as soon as we reach a
// live entry. O(expired) per call — amortized O(1) per insertion.
func (c *RecentMessageIDCache) prune() {
	now := time.Now()
	i := 0
	for i < len(c.expiry) && c.expiry[i].expiresAt.Before(now) {
		key := c.expiry[i].key
		// Only remove the map entry if the key hasn't been refreshed since
		// this queue entry was added (i.e., its actual expiry has passed).
		if e, ok := c.entries[key]; ok && e.seenAt.Add(recentMessageIDTTL).Before(now) {
			delete(c.entries, key)
		}
		i++
	}
	// Slide the queue forward, reusing the backing array to avoid allocation.
	c.expiry = c.expiry[i:]
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
