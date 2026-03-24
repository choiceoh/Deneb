package session

// LifecyclePhase represents a phase in the session lifecycle.
type LifecyclePhase string

const (
	PhaseStart LifecyclePhase = "start"
	PhaseEnd   LifecyclePhase = "end"
	PhaseError LifecyclePhase = "error"
)

// LifecycleEvent represents a session lifecycle state transition event.
type LifecycleEvent struct {
	Phase      LifecyclePhase `json:"phase"`
	Ts         int64          `json:"ts"`
	StopReason string         `json:"stopReason,omitempty"`
	Aborted    bool           `json:"aborted,omitempty"`
	StartedAt  *int64         `json:"startedAt,omitempty"`
	EndedAt    *int64         `json:"endedAt,omitempty"`
}

// LifecycleSnapshot captures the derived session state after applying an event.
type LifecycleSnapshot struct {
	Status         RunStatus `json:"status"`
	StartedAt      *int64    `json:"startedAt,omitempty"`
	EndedAt        *int64    `json:"endedAt,omitempty"`
	RuntimeMs      *int64    `json:"runtimeMs,omitempty"`
	UpdatedAt      *int64    `json:"updatedAt,omitempty"`
	AbortedLastRun bool      `json:"abortedLastRun"`
}

// isFiniteTimestamp checks whether a timestamp pointer is valid (non-nil, > 0).
func isFiniteTimestamp(v *int64) bool {
	return v != nil && *v > 0
}

// resolveLifecyclePhase validates a phase value. Returns nil for unknown phases.
func resolveLifecyclePhase(phase LifecyclePhase) *LifecyclePhase {
	switch phase {
	case PhaseStart, PhaseEnd, PhaseError:
		return &phase
	default:
		return nil
	}
}

// resolveTerminalStatus determines the terminal RunStatus from an event.
func resolveTerminalStatus(event LifecycleEvent) RunStatus {
	if event.Phase == PhaseError {
		return StatusFailed
	}
	if event.StopReason == "aborted" {
		return StatusKilled
	}
	if event.Aborted {
		return StatusTimeout
	}
	return StatusDone
}

// resolveLifecycleStartedAt resolves startedAt with a three-tier fallback:
// event.StartedAt → existing → event.Ts.
func resolveLifecycleStartedAt(existingStartedAt *int64, event LifecycleEvent) *int64 {
	if isFiniteTimestamp(event.StartedAt) {
		return event.StartedAt
	}
	if isFiniteTimestamp(existingStartedAt) {
		return existingStartedAt
	}
	if event.Ts > 0 {
		return &event.Ts
	}
	return nil
}

// resolveLifecycleEndedAt resolves endedAt with a two-tier fallback:
// event.EndedAt → event.Ts.
func resolveLifecycleEndedAt(event LifecycleEvent) *int64 {
	if isFiniteTimestamp(event.EndedAt) {
		return event.EndedAt
	}
	if event.Ts > 0 {
		return &event.Ts
	}
	return nil
}

// resolveRuntimeMs computes runtime from timestamps or falls back to the existing value.
// Clamps the result to >= 0.
func resolveRuntimeMs(startedAt, endedAt, existingRuntimeMs *int64) *int64 {
	if isFiniteTimestamp(startedAt) && isFiniteTimestamp(endedAt) {
		ms := *endedAt - *startedAt
		if ms < 0 {
			ms = 0
		}
		return &ms
	}
	if existingRuntimeMs != nil && *existingRuntimeMs >= 0 {
		return existingRuntimeMs
	}
	return nil
}

// cloneInt64Ptr returns a new pointer with the same value, breaking aliasing.
func cloneInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// DeriveLifecycleSnapshot computes the new session state from an existing session
// and a lifecycle event. Mirrors the logic in deriveGatewaySessionLifecycleSnapshot().
func DeriveLifecycleSnapshot(existing *Session, event LifecycleEvent) LifecycleSnapshot {
	phase := resolveLifecyclePhase(event.Phase)
	if phase == nil {
		return LifecycleSnapshot{}
	}

	var existingStartedAt, existingRuntimeMs *int64
	var existingUpdatedAt *int64
	if existing != nil {
		existingStartedAt = existing.StartedAt
		existingRuntimeMs = existing.RuntimeMs
		if existing.UpdatedAt != 0 {
			ua := existing.UpdatedAt
			existingUpdatedAt = &ua
		}
	}

	if *phase == PhaseStart {
		startedAt := resolveLifecycleStartedAt(existingStartedAt, event)
		updatedAt := cloneInt64Ptr(startedAt)
		if updatedAt == nil {
			updatedAt = existingUpdatedAt
		}
		return LifecycleSnapshot{
			Status:         StatusRunning,
			StartedAt:      startedAt,
			EndedAt:        nil,
			RuntimeMs:      nil,
			UpdatedAt:      updatedAt,
			AbortedLastRun: false,
		}
	}

	// PhaseEnd or PhaseError.
	startedAt := resolveLifecycleStartedAt(existingStartedAt, event)
	endedAt := resolveLifecycleEndedAt(event)
	updatedAt := cloneInt64Ptr(endedAt)
	if updatedAt == nil {
		updatedAt = existingUpdatedAt
	}
	status := resolveTerminalStatus(event)
	return LifecycleSnapshot{
		Status:         status,
		StartedAt:      startedAt,
		EndedAt:        endedAt,
		RuntimeMs:      resolveRuntimeMs(startedAt, endedAt, existingRuntimeMs),
		UpdatedAt:      updatedAt,
		AbortedLastRun: status == StatusKilled,
	}
}
