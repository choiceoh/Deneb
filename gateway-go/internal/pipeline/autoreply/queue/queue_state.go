// queue_state.go — Followup queue state management.
// Mirrors src/auto-reply/reply/queue/state.ts (88 LOC).
package queue

import (
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

const (
	DefaultFollowupDebounceMs = 0
	DefaultFollowupCap        = 20
	// Drop policy is always summarize for the single-user Telegram bot.
	DefaultFollowupDrop = types.FollowupDropSummarize
)

// FollowupQueueState tracks the runtime state of a single followup queue.
// All field access must be guarded by mu to prevent races between the
// enqueue caller and the drain goroutine.
type FollowupQueueState struct {
	mu             sync.Mutex                `json:"-"`
	Items          []types.FollowupRun       `json:"items"`
	Draining       bool                      `json:"draining"`
	LastEnqueuedAt int64                     `json:"lastEnqueuedAt"`
	Mode           types.FollowupQueueMode   `json:"mode"`
	DebounceMs     int                       `json:"debounceMs"`
	Cap            int                       `json:"cap"`
	DropPolicy     types.FollowupDropPolicy  `json:"dropPolicy"`
	DroppedCount   int                       `json:"droppedCount"`
	SummaryLines   []string                  `json:"summaryLines"`
	LastRun        *types.FollowupRunContext `json:"lastRun,omitempty"`
}

// tryStartDrain atomically checks if the queue is already draining.
// If not, marks it as draining and returns true; otherwise returns false.
func (q *FollowupQueueState) tryStartDrain() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.Draining {
		return false
	}
	q.Draining = true
	return true
}

// finishDrain marks the queue as no longer draining and reports whether it's empty.
func (q *FollowupQueueState) finishDrain() (empty bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Draining = false
	return len(q.Items) == 0 && q.DroppedCount == 0
}

// resetDraining clears the draining flag and reports whether the queue has pending work.
func (q *FollowupQueueState) resetDraining() (needsKick bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Draining = false
	return len(q.Items) > 0 || q.DroppedCount > 0
}

// peekWork reports whether the queue has work and returns the debounce setting.
func (q *FollowupQueueState) peekWork() (hasWork bool, debounceMs int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.Items) > 0 || q.DroppedCount > 0, q.DebounceMs
}

// checkCrossChannel reports whether items target different routing destinations.
func (q *FollowupQueueState) checkCrossChannel() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return hasCrossChannelItems(q.Items)
}

// dequeueFirst removes and returns the first item, or reports false if empty.
func (q *FollowupQueueState) dequeueFirst() (types.FollowupRun, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.Items) == 0 {
		return types.FollowupRun{}, false
	}
	item := q.Items[0]
	q.Items = q.Items[1:]
	return item, true
}

// touchEnqueue updates LastEnqueuedAt to now.
func (q *FollowupQueueState) touchEnqueue() {
	q.mu.Lock()
	q.LastEnqueuedAt = time.Now().UnixMilli()
	q.mu.Unlock()
}

// clearSummary resets the dropped/summary state.
func (q *FollowupQueueState) clearSummary() {
	q.mu.Lock()
	clearFollowupSummaryState(q)
	q.mu.Unlock()
}

// depth returns the number of items.
func (q *FollowupQueueState) depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.Items)
}

// itemCount returns items count plus dropped count.
func (q *FollowupQueueState) itemCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.Items) + q.DroppedCount
}

// applySettings updates runtime settings under lock.
func (q *FollowupQueueState) applySettings(settings types.FollowupQueueSettings) {
	q.mu.Lock()
	applyFollowupQueueSettings(q, settings)
	q.mu.Unlock()
}

// snapshotSummaryDrain takes a snapshot for summary draining.
// Dequeues the first item as routing source. Returns false if no summary available.
func (q *FollowupQueueState) snapshotSummaryDrain() (summaryPrompt string, lastRun *types.FollowupRunContext, item types.FollowupRun, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.DroppedCount == 0 || len(q.SummaryLines) == 0 {
		return "", nil, types.FollowupRun{}, false
	}
	summaryPrompt = buildFollowupSummaryPrompt(q)
	lastRun = q.LastRun
	if summaryPrompt == "" || lastRun == nil || len(q.Items) == 0 {
		return "", nil, types.FollowupRun{}, false
	}
	item = q.Items[0]
	q.Items = q.Items[1:]
	return summaryPrompt, lastRun, item, true
}

// snapshotCollect takes a snapshot of items for batch draining.
func (q *FollowupQueueState) snapshotCollect() (items []types.FollowupRun, lastRun *types.FollowupRunContext, summary string, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.Items) == 0 {
		return nil, nil, "", false
	}
	items = make([]types.FollowupRun, len(q.Items))
	copy(items, q.Items)
	lastRun = q.LastRun
	if len(items) > 0 && items[len(items)-1].Run != nil {
		lastRun = items[len(items)-1].Run
	}
	summary = buildFollowupSummaryPrompt(q)
	return items, lastRun, summary, true
}

// consumeCollected removes up to n items from the front and optionally clears summary state.
func (q *FollowupQueueState) consumeCollected(n int, clearSummaryState bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n > len(q.Items) {
		n = len(q.Items)
	}
	q.Items = q.Items[n:]
	if clearSummaryState {
		clearFollowupSummaryState(q)
	}
}

// FollowupQueueRegistry manages all followup queues (one per session key).
type FollowupQueueRegistry struct {
	mu     sync.Mutex
	queues map[string]*FollowupQueueState
}

// NewFollowupQueueRegistry creates a new followup queue registry.
func NewFollowupQueueRegistry() *FollowupQueueRegistry {
	return &FollowupQueueRegistry{
		queues: make(map[string]*FollowupQueueState),
	}
}

// Existing returns an existing queue state or nil.
// Callers must lock the returned queue before accessing fields.
func (r *FollowupQueueRegistry) Existing(key string) *FollowupQueueState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key == "" {
		return nil
	}
	return r.queues[key]
}

// GetOrCreate returns an existing queue or creates one with the given settings.
// The returned queue is NOT locked; callers must lock before accessing fields.
func (r *FollowupQueueRegistry) GetOrCreate(key string, settings types.FollowupQueueSettings) *FollowupQueueState {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.queues[key]; ok {
		existing.applySettings(settings)
		return existing
	}

	debounce := DefaultFollowupDebounceMs
	if settings.DebounceMs > 0 {
		debounce = settings.DebounceMs
	}
	cap := DefaultFollowupCap
	if settings.Cap > 0 {
		cap = settings.Cap
	}

	created := &FollowupQueueState{
		Items:        make([]types.FollowupRun, 0),
		Mode:         types.FollowupModeCollect,
		DebounceMs:   debounce,
		Cap:          cap,
		DropPolicy:   DefaultFollowupDrop,
		SummaryLines: make([]string, 0),
	}
	r.queues[key] = created
	return created
}

// Clear removes a queue and returns the number of items that were cleared.
func (r *FollowupQueueRegistry) Clear(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	q, ok := r.queues[key]
	if !ok {
		return 0
	}
	cleared := q.itemCount()
	delete(r.queues, key)
	return cleared
}

// Delete removes a queue from the registry.
func (r *FollowupQueueRegistry) Delete(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.queues, key)
}

// Keys returns all registered queue keys.
func (r *FollowupQueueRegistry) Keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.queues))
	for k := range r.queues {
		keys = append(keys, k)
	}
	return keys
}

// Depth returns the number of items in a queue (0 if not found).
func (r *FollowupQueueRegistry) Depth(key string) int {
	r.mu.Lock()
	q, ok := r.queues[key]
	r.mu.Unlock()
	if !ok {
		return 0
	}
	return q.depth()
}

// applyFollowupQueueSettings updates a queue's runtime settings.
// Mode and drop policy are fixed (collect + summarize); only debounce
// and cap are adjustable.
// Caller must hold q.mu.
func applyFollowupQueueSettings(state *FollowupQueueState, settings types.FollowupQueueSettings) {
	if settings.DebounceMs > 0 {
		state.DebounceMs = settings.DebounceMs
	}
	if settings.Cap > 0 {
		state.Cap = settings.Cap
	}
}
