// queue_state.go — Followup queue state management.
// Mirrors src/auto-reply/reply/queue/state.ts (88 LOC).
package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"sync"
)

const (
	DefaultFollowupDebounceMs = 0
	DefaultFollowupCap        = 20
	DefaultFollowupDrop       = types.FollowupDropSummarize
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

// Lock acquires the per-queue mutex.
func (q *FollowupQueueState) Lock() { q.mu.Lock() }

// Unlock releases the per-queue mutex.
func (q *FollowupQueueState) Unlock() { q.mu.Unlock() }

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

// GetExisting returns an existing queue state or nil.
// Callers must lock the returned queue before accessing fields.
func (r *FollowupQueueRegistry) GetExisting(key string) *FollowupQueueState {
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
		// Apply settings under the per-queue lock.
		existing.Lock()
		applyFollowupQueueSettings(existing, settings)
		existing.Unlock()
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
	drop := DefaultFollowupDrop
	if settings.DropPolicy != "" {
		drop = settings.DropPolicy
	}

	created := &FollowupQueueState{
		Items:        make([]types.FollowupRun, 0),
		Mode:         settings.Mode,
		DebounceMs:   debounce,
		Cap:          cap,
		DropPolicy:   drop,
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
	q.Lock()
	cleared := len(q.Items) + q.DroppedCount
	q.Unlock()
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
	q.Lock()
	n := len(q.Items)
	q.Unlock()
	return n
}

// applyFollowupQueueSettings updates a queue's runtime settings.
// Caller must hold q.mu.
func applyFollowupQueueSettings(state *FollowupQueueState, settings types.FollowupQueueSettings) {
	if settings.Mode != "" {
		state.Mode = settings.Mode
	}
	if settings.DebounceMs > 0 {
		state.DebounceMs = settings.DebounceMs
	}
	if settings.Cap > 0 {
		state.Cap = settings.Cap
	}
	if settings.DropPolicy != "" {
		state.DropPolicy = settings.DropPolicy
	}
}
