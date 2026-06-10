package modelrole

import (
	"sync"
	"time"
)

// Circuit-breaker thresholds. A model that fails unhealthyStreak times in a
// row is considered unhealthy for unhealthyCooldown after its last failure;
// the chat pipeline then skips it and goes straight to the fallback chain
// (saving the user the dead model's stall timeout). The cooldown auto-closes
// the breaker so a recovered model is retried without operator action.
const (
	unhealthyStreak   = 3
	unhealthyCooldown = 2 * time.Minute
)

// modelHealth tracks consecutive failures for one model.
type modelHealth struct {
	streak      int
	lastFailure time.Time
}

// healthState holds per-model failure streaks. It lives outside Registry.mu
// (independent lock — never hold both) because health updates happen on the
// hot run path and must not contend with config resolution.
type healthState struct {
	mu     sync.Mutex
	models map[string]*modelHealth
}

// RecordModelFailure notes a hard failure (error or stall) for a model.
// Keyed by bare model name — the same key the fallback chain dedupes on.
func (r *Registry) RecordModelFailure(model string) {
	if model == "" {
		return
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	h := r.health.models[model]
	if h == nil {
		h = &modelHealth{}
		r.health.models[model] = h
	}
	h.streak++
	h.lastFailure = time.Now()
}

// RecordModelSuccess resets a model's failure streak (closes the breaker).
func (r *Registry) RecordModelSuccess(model string) {
	if model == "" {
		return
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	delete(r.health.models, model)
}

// ModelUnhealthy reports whether a model's breaker is open: at least
// unhealthyStreak consecutive failures with the latest inside the cooldown
// window. Outside the window the breaker half-opens (returns false) so the
// model gets retried; a success then resets the streak, a failure re-arms it.
func (r *Registry) ModelUnhealthy(model string) bool {
	if model == "" {
		return false
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	h := r.health.models[model]
	return h != nil && h.streak >= unhealthyStreak && time.Since(h.lastFailure) < unhealthyCooldown
}
