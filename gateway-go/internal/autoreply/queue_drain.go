// queue_drain.go — Followup queue drain scheduling and execution.
// Mirrors src/auto-reply/reply/queue/drain.ts (203 LOC).
package autoreply

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// FollowupDrainCallback runs a single followup item.
type FollowupDrainCallback func(run FollowupRun) error

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
		q.Draining = false
		if len(q.Items) > 0 || q.DroppedCount > 0 {
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
	if queue.Draining {
		return
	}
	queue.Draining = true
	s.callbacks.Set(key, runFollowup)

	go func() {
		defer func() {
			queue.Draining = false
			if len(queue.Items) == 0 && queue.DroppedCount == 0 {
				s.registry.Delete(key)
			} else {
				// Re-schedule if items remain.
				s.ScheduleDrain(key, runFollowup)
			}
		}()

		for len(queue.Items) > 0 || queue.DroppedCount > 0 {
			// Wait for debounce.
			if queue.DebounceMs > 0 {
				time.Sleep(time.Duration(queue.DebounceMs) * time.Millisecond)
			}

			if queue.Mode == FollowupModeCollect {
				if !s.drainCollect(queue, runFollowup) {
					break
				}
				continue
			}

			// Drain summary prompt if accumulated.
			if queue.DroppedCount > 0 && len(queue.SummaryLines) > 0 {
				summaryPrompt := buildFollowupSummaryPrompt(queue)
				if summaryPrompt != "" && queue.LastRun != nil {
					if len(queue.Items) > 0 {
						item := queue.Items[0]
						queue.Items = queue.Items[1:]
						err := runFollowup(FollowupRun{
							Prompt:               summaryPrompt,
							Run:                  queue.LastRun,
							EnqueuedAt:           time.Now().UnixMilli(),
							OriginatingChannel:   item.OriginatingChannel,
							OriginatingTo:        item.OriginatingTo,
							OriginatingAccountID: item.OriginatingAccountID,
							OriginatingThreadID:  item.OriginatingThreadID,
						})
						if err != nil {
							s.logError(fmt.Sprintf("followup queue drain summary failed for %s: %s", key, err))
							break
						}
					}
					clearFollowupSummaryState(queue)
					continue
				}
			}

			// Drain next individual item.
			if len(queue.Items) == 0 {
				break
			}
			item := queue.Items[0]
			queue.Items = queue.Items[1:]
			if err := runFollowup(item); err != nil {
				queue.LastEnqueuedAt = time.Now().UnixMilli()
				s.logError(fmt.Sprintf("followup queue drain failed for %s: %s", key, err))
				break
			}
		}
	}()
}

// drainCollect processes items in collect mode (batch all into a single prompt).
func (s *FollowupDrainService) drainCollect(queue *FollowupQueueState, runFollowup FollowupDrainCallback) bool {
	if len(queue.Items) == 0 {
		return false
	}

	items := make([]FollowupRun, len(queue.Items))
	copy(items, queue.Items)

	run := queue.LastRun
	if len(items) > 0 && items[len(items)-1].Run != nil {
		run = items[len(items)-1].Run
	}
	if run == nil {
		return false
	}

	// Build collected prompt.
	var lines []string
	lines = append(lines, "[Queued messages while agent was busy]")
	for i, item := range items {
		lines = append(lines, fmt.Sprintf("---\nQueued #%d\n%s", i+1, strings.TrimSpace(item.Prompt)))
	}

	summary := buildFollowupSummaryPrompt(queue)
	if summary != "" {
		lines = append(lines, "---", summary)
	}

	// Resolve routing from items.
	routing := resolveOriginRoutingMetadata(items)

	err := runFollowup(FollowupRun{
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

	// Remove consumed items.
	queue.Items = queue.Items[len(items):]
	if summary != "" {
		clearFollowupSummaryState(queue)
	}
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
func resolveOriginRoutingMetadata(items []FollowupRun) originRoutingMetadata {
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
func clearFollowupSummaryState(state *FollowupQueueState) {
	state.DroppedCount = 0
	state.SummaryLines = state.SummaryLines[:0]
}
