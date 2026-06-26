package modelrole

// Tuned per-model parameter overrides written by the background model tuner
// (internal/ai/modeltuner) and consumed by the chat pipeline. In-memory only:
// the tuner re-derives and re-applies them from the agent-log scorecard on
// every cycle, so a restart loses at most one tuning interval.

// SetTunedMaxTokens records a tuned max-output-tokens floor for a model.
// Zero or negative clears the entry.
func (r *Registry) SetTunedMaxTokens(model string, tokens int) {
	if model == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if tokens <= 0 {
		delete(r.tunedMaxTokens, model)
		return
	}
	r.tunedMaxTokens[model] = tokens
}

// TunedMaxTokens returns the tuned max-output-tokens floor for a model,
// or 0 when none is set. Callers must treat it as a floor (raise-only) and
// never let it override an explicit per-request value.
func (r *Registry) TunedMaxTokens(model string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunedMaxTokens[model]
}

// SetTunedMaxSimpleRunes records a tuned effort-router MaxSimpleRunes gate for a
// model, written by the background adaptive-effort nudge (DENEB_ADAPTIVE_EFFORT_
// TUNE). It is the runtime-writable counterpart to the static profile constant:
// RoutingProfileForModel layers it on top of the resolved routing profile.
// Zero or negative clears the entry (gate falls back to the profile value).
// The nudge clamps to a sane band before calling here; this only stores.
func (r *Registry) SetTunedMaxSimpleRunes(model string, runes int) {
	if model == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if runes <= 0 {
		delete(r.tunedMaxSimpleRunes, model)
		return
	}
	r.tunedMaxSimpleRunes[model] = runes
}

// TunedMaxSimpleRunes returns the tuned effort-router MaxSimpleRunes gate for a
// model, or 0 when none is set (the profile value stands).
func (r *Registry) TunedMaxSimpleRunes(model string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunedMaxSimpleRunes[model]
}
