// queue_drain.go — Followup queue drain scheduling and execution.
// Mirrors src/auto-reply/reply/queue/drain.ts (203 LOC).
package queue

import (
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"strings"
	"sync"
	"time"
)

// FollowupDrainCallback runs a single followup item.
type FollowupDrainCallback func(run types.FollowupRun) error

// FollowupDrainCallbacks stores the most recent drain callback per queue key
// so that enqueue can restart a drain that finished and deleted the queue.
type FollowupDrainCallbacks struct {
	mu        sync.Mutex
	callbacks map[string]FollowupDrainCallback
}

// NewFollowupDrainCallbacks creates a new drain callback registry.
func NewFollowupDrainCallbacks() *FollowupDrainCallbacks {
	return &FollowupDrainCallbacks{
		callbacks: make(map[string]FollowupDrainCallback),
	}
}

// Set stores a callback for a queue key.
func (d *FollowupDrainCallbacks) Set(key string, cb FollowupDrainCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks[key] = cb
}

// Get returns the callback for a queue key, or nil.
func (d *FollowupDrainCallbacks) Get(key string) FollowupDrainCallback {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.callbacks[key]
}

// Delete removes the callback for a queue key.
func (d *FollowupDrainCallbacks) Delete(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.callbacks, key)
}

// backoffDelays defines exponential backoff durations for consecutive failures.
var backoffDelays = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

// maxConsecutiveFailures caps the backoff index.
const maxConsecutiveFailures = 5

// FollowupDrainService manages followup queue draining.
type FollowupDrainService struct {
	registry  *FollowupQueueRegistry
	callbacks *FollowupDrainCallbacks
	logError  func(msg string)
}

// NewFollowupDrainService creates a new drain service.
func NewFollowupDrainService(
	registry *FollowupQueueRegistry,
	logError func(msg string),
) *FollowupDrainService {
	return &FollowupDrainService{
		registry:  registry,
		callbacks: NewFollowupDrainCallbacks(),
		logError:  logError,
	}
}

// KickIfIdle restarts the drain for a key if currently idle, using the stored callback.
func (s *FollowupDrainService) KickIfIdle(key string) {
	cb := s.callbacks.Get(key)
	if cb == nil {
		return
	}
	s.ScheduleDrain(key, cb)
}

// ResetDrainState resets followup queue state after an in-process restart.
// Interrupted drain coroutines leave draining=true, permanently blocking new drains.
func (s *FollowupDrainService) ResetDrainState() {
	for _, key := range s.registry.Keys() {
		q := s.registry.GetExisting(key)
		if q == nil {
			continue
		}
		q.Lock()
		q.Draining = false
		needsKick := len(q.Items) > 0 || q.DroppedCount > 0
		q.Unlock()
		if needsKick {
			s.KickIfIdle(key)
		}
	}
}

// ScheduleDrain starts a drain goroutine for the given queue key.
// If the queue is already draining, this is a no-op.
func (s *FollowupDrainService) ScheduleDrain(key string, runFollowup FollowupDrainCallback) {
	queue := s.registry.GetExisting(key)
	if queue == nil {
		return
	}
	queue.Lock()
	if queue.Draining {
		queue.Unlock()
		return
	}
	queue.Draining = true
	queue.Unlock()
	s.callbacks.Set(key, runFollowup)

	go s.drainLoop(key, queue, runFollowup)
}

// drainLoop is the main drain goroutine. It holds/releases the per-queue lock
// around field access but releases it before executing callbacks.
func (s *FollowupDrainService) drainLoop(key string, queue *FollowupQueueState, runFollowup FollowupDrainCallback) {
	consecutiveFailures := 0
	collectForceIndividual := false
	reschedule := true

	defer func() {
		queue.Lock()
		queue.Draining = false
		empty := len(queue.Items) == 0 && queue.DroppedCount == 0
		queue.Unlock()
		if empty {
			s.registry.Delete(key)
		} else if reschedule {
			s.ScheduleDrain(key, runFollowup)
		}
	}()

	for {
		// Snapshot loop condition under lock.
		queue.Lock()
		hasWork := len(queue.Items) > 0 || queue.DroppedCount > 0
		debounceMs := queue.DebounceMs
		queue.Unlock()
		if !hasWork {
			break
		}

		// Wait for debounce (unlocked — allows enqueue during sleep).
		if debounceMs > 0 {
			time.Sleep(time.Duration(debounceMs) * time.Millisecond)
		}

		// Apply exponential backoff on consecutive failures.
		if consecutiveFailures > 0 {
			idx := consecutiveFailures - 1
			if idx >= len(backoffDelays) {
				idx = len(backoffDelays) - 1
			}
			time.Sleep(backoffDelays[idx])
		}

		// --- Collect mode ---
		queue.Lock()
		mode := queue.Mode
		queue.Unlock()

		if mode == types.FollowupModeCollect {
			if !collectForceIndividual {
				queue.Lock()
				isCross := hasCrossChannelItems(queue.Items)
				queue.Unlock()
				if isCross {
					collectForceIndividual = true
				}
			}

			if !collectForceIndividual {
				ok := s.drainCollect(queue, runFollowup)
				if !ok {
					consecutiveFailures++
					if consecutiveFailures >= maxConsecutiveFailures {
						s.logError(fmt.Sprintf("followup queue drain giving up for %s after %d failures", key, consecutiveFailures))
						reschedule = false
						break
					}
					continue
				}
				consecutiveFailures = 0
				continue
			}
			// Fall through to individual drain for cross-channel items.
		}

		// --- Summary drain ---
		drained := s.trySummaryDrain(key, queue, runFollowup)
		if drained {
			consecutiveFailures = 0
			continue
		}

		// --- Individual item drain ---
		queue.Lock()
		if len(queue.Items) == 0 {
			queue.Unlock()
			break
		}
		item := queue.Items[0]
		queue.Items = queue.Items[1:]
		queue.Unlock()

		// Execute callback without holding the lock.
		if err := runFollowup(item); err != nil {
			queue.Lock()
			queue.LastEnqueuedAt = time.Now().UnixMilli()
			queue.Unlock()
			consecutiveFailures++
			s.logError(fmt.Sprintf("followup queue drain failed for %s: %s", key, err))
			if consecutiveFailures >= maxConsecutiveFailures {
				reschedule = false
				break
			}
			continue
		}
		consecutiveFailures = 0
	}
}

// trySummaryDrain attempts to drain the summary prompt if accumulated.
// Returns true if a summary was drained (success or failure).
func (s *FollowupDrainService) trySummaryDrain(key string, queue *FollowupQueueState, runFollowup FollowupDrainCallback) bool {
	queue.Lock()
	if queue.DroppedCount == 0 || len(queue.SummaryLines) == 0 {
		queue.Unlock()
		return false
	}
	summaryPrompt := buildFollowupSummaryPrompt(queue)
	lastRun := queue.LastRun
	if summaryPrompt == "" || lastRun == nil || len(queue.Items) == 0 {
		queue.Unlock()
		return false
	}
	item := queue.Items[0]
	queue.Items = queue.Items[1:]
	queue.Unlock()

	err := runFollowup(types.FollowupRun{
		Prompt:               summaryPrompt,
		Run:                  lastRun,
		EnqueuedAt:           time.Now().UnixMilli(),
		OriginatingChannel:   item.OriginatingChannel,
		OriginatingTo:        item.OriginatingTo,
		OriginatingAccountID: item.OriginatingAccountID,
		OriginatingThreadID:  item.OriginatingThreadID,
	})
	if err != nil {
		s.logError(fmt.Sprintf("followup queue drain summary failed for %s: %s", key, err))
		return true // still consumed the attempt
	}
	queue.Lock()
	clearFollowupSummaryState(queue)
	queue.Unlock()
	return true
}

// drainCollect processes items in collect mode (batch all into a single prompt).
func (s *FollowupDrainService) drainCollect(queue *FollowupQueueState, runFollowup FollowupDrainCallback) bool {
	queue.Lock()
	if len(queue.Items) == 0 {
		queue.Unlock()
		return false
	}

	// Snapshot items for the batch.
	items := make([]types.FollowupRun, len(queue.Items))
	copy(items, queue.Items)
	run := queue.LastRun
	if len(items) > 0 && items[len(items)-1].Run != nil {
		run = items[len(items)-1].Run
	}
	summary := buildFollowupSummaryPrompt(queue)
	queue.Unlock()

	if run == nil {
		return false
	}

	// Build collected prompt (no lock needed, working on snapshot).
	var lines []string
	lines = append(lines, "[Queued messages while agent was busy]")
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("---\nQueued #%d\n%s", i+1, strings.TrimSpace(item.Prompt)))
	}
	if summary != "" {
		lines = append(lines, "---", summary)
	}
	routing := resolveOriginRoutingMetadata(items)

	// Execute callback without holding the lock.
	err := runFollowup(types.FollowupRun{
		Prompt:               strings.Join(lines, "\n"),
		Run:                  run,
		EnqueuedAt:           time.Now().UnixMilli(),
		OriginatingChannel:   routing.OriginatingChannel,
		OriginatingTo:        routing.OriginatingTo,
		OriginatingAccountID: routing.OriginatingAccountID,
		OriginatingThreadID:  routing.OriginatingThreadID,
	})
	if err != nil {
		return false
	}

	// Remove consumed items under lock.
	queue.Lock()
	// Items may have been appended during the callback; only remove the
	// ones we consumed (up to snapshotLen).
	snapshotLen := len(items)
	if snapshotLen > len(queue.Items) {
		snapshotLen = len(queue.Items)
	}
	queue.Items = queue.Items[snapshotLen:]
	if summary != "" {
		clearFollowupSummaryState(queue)
	}
	queue.Unlock()
	return true
}

// originRoutingMetadata holds routing fields extracted from followup runs.
type originRoutingMetadata struct {
	OriginatingChannel   string
	OriginatingTo        string
	OriginatingAccountID string
	OriginatingThreadID  string
}

// resolveOriginRoutingMetadata picks the first non-empty routing from items.
func resolveOriginRoutingMetadata(items []types.FollowupRun) originRoutingMetadata {
	var result originRoutingMetadata
	for _, item := range items {
		if result.OriginatingChannel == "" && item.OriginatingChannel != "" {
			result.OriginatingChannel = item.OriginatingChannel
		}
		if result.OriginatingTo == "" && item.OriginatingTo != "" {
			result.OriginatingTo = item.OriginatingTo
		}
		if result.OriginatingAccountID == "" && item.OriginatingAccountID != "" {
			result.OriginatingAccountID = item.OriginatingAccountID
		}
		if result.OriginatingThreadID == "" && item.OriginatingThreadID != "" {
			result.OriginatingThreadID = item.OriginatingThreadID
		}
	}
	return result
}

// buildFollowupSummaryPrompt creates a summary prompt from dropped items.
// Caller must hold queue.mu.
func buildFollowupSummaryPrompt(state *FollowupQueueState) string {
	if state.DroppedCount == 0 || len(state.SummaryLines) == 0 {
		return ""
	}
	noun := "message"
	if state.DroppedCount != 1 {
		noun = "messages"
	}
	return fmt.Sprintf("[%d earlier %s were dropped from the queue]\nSummary:\n%s",
		state.DroppedCount, noun, strings.Join(state.SummaryLines, "\n"))
}

// clearFollowupSummaryState resets the summary/dropped state.
// Caller must hold queue.mu.
func clearFollowupSummaryState(state *FollowupQueueState) {
	state.DroppedCount = 0
	state.SummaryLines = state.SummaryLines[:0]
}

// hasCrossChannelItems returns true if items target different channel/to/account/thread combinations.
// Caller must hold queue.mu.
func hasCrossChannelItems(items []types.FollowupRun) bool {
	if len(items) <= 1 {
		return false
	}
	ref := crossChannelKey(items[0])
	for _, item := range items[1:] {
		if crossChannelKey(item) != ref {
			return true
		}
	}
	return false
}

// crossChannelKey builds a routing key for cross-channel comparison.
func crossChannelKey(item types.FollowupRun) string {
	return item.OriginatingChannel + "|" + item.OriginatingTo + "|" +
		item.OriginatingAccountID + "|" + item.OriginatingThreadID
}
