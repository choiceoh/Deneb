package modelrole

import (
	"strings"
	"sync"
	"time"
)

// Circuit-breaker thresholds. A model that fails unhealthyStreak times in a
// row is considered unhealthy for unhealthyCooldown after its last failure;
// the chat pipeline then skips it and goes straight to the fallback chain
// (saving the user the dead model's stall timeout). The cooldown must be longer
// than one or two stall waits; a 2-minute window half-opened before the next
// production run and made every request pay k3's 180s idle stall again.
// The cooldown auto-closes the breaker so a recovered model is retried without
// operator action.
const (
	unhealthyStreak   = 3
	unhealthyCooldown = 15 * time.Minute
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
	// engineDown maps a serving engine's endpoint to the models it answers
	// for, while a readiness probe reports that engine refusing requests.
	// Kept apart from the streaks: a streak is inferred from failures a caller
	// already paid for; this is reported before anyone pays.
	engineDown map[string]map[string]bool
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
//
// A model whose serving engine a readiness probe reports down is unhealthy
// too, with no streak required — see SetEngineDown.
func (r *Registry) ModelUnhealthy(model string) bool {
	if model == "" {
		return false
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	h := r.health.models[model]
	if h != nil && h.streak >= unhealthyStreak && time.Since(h.lastFailure) < unhealthyCooldown {
		return true
	}
	return r.engineDownLocked(model)
}

// SetEngineDown records the models a serving engine answers for while its
// readiness probe says it accepts nothing. Pass no models once it is back.
//
// While a model is listed, ModelUnhealthy and EngineDown report it: the chat
// pipeline goes straight to the fallback chain and LLM clients stop retrying
// it. Those retries cannot succeed — the engine refused before a token existed
// — and on 2026-09-14 they cost about 70 seconds per call, doubled by the
// run-level transient replay, on every turn that reached the dead engine.
//
// A model leaving the set also loses its failure streak. The streak was built
// against an engine that is no longer the one answering, and keeping it would
// hold traffic on the fallback for up to unhealthyCooldown after the engine
// came back — local serving lost for nothing.
func (r *Registry) SetEngineDown(endpoint string, models []string) {
	if r == nil || endpoint == "" {
		return
	}
	next := make(map[string]bool, len(models))
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			next[m] = true
		}
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	prev := r.health.engineDown[endpoint]
	if len(next) == 0 {
		delete(r.health.engineDown, endpoint)
	} else {
		if r.health.engineDown == nil {
			r.health.engineDown = make(map[string]map[string]bool)
		}
		r.health.engineDown[endpoint] = next
	}
	for m := range prev {
		// Another engine may still list the same name; only a model no engine
		// reports down is back.
		if !next[m] && !r.engineDownLocked(m) {
			delete(r.health.models, m)
		}
	}
}

// EngineDown reports whether a readiness probe currently has model's serving
// engine refusing requests. Unlike ModelUnhealthy it ignores failure streaks:
// it answers "is retrying this pointless right now", which a streak cannot.
func (r *Registry) EngineDown(model string) bool {
	if r == nil || model == "" {
		return false
	}
	r.health.mu.Lock()
	defer r.health.mu.Unlock()
	return r.engineDownLocked(model)
}

// engineDownLocked requires r.health.mu.
func (r *Registry) engineDownLocked(model string) bool {
	for _, served := range r.health.engineDown {
		if served[model] {
			return true
		}
	}
	return false
}
