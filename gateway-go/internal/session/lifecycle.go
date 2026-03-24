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
}

// LifecycleSnapshot captures the derived session state after applying an event.
type LifecycleSnapshot struct {
	Status    RunStatus `json:"status"`
	StartedAt *int64    `json:"startedAt,omitempty"`
	EndedAt   *int64    `json:"endedAt,omitempty"`
	RuntimeMs *int64    `json:"runtimeMs,omitempty"`
}

// DeriveLifecycleSnapshot computes the new session state from an existing session
// and a lifecycle event. Mirrors the logic in deriveGatewaySessionLifecycleSnapshot().
func DeriveLifecycleSnapshot(existing *Session, event LifecycleEvent) LifecycleSnapshot {
	snap := LifecycleSnapshot{}

	switch event.Phase {
	case PhaseStart:
		snap.Status = StatusRunning
		snap.StartedAt = &event.Ts

	case PhaseEnd:
		if event.StopReason == "aborted" {
			snap.Status = StatusKilled
		} else if event.Aborted {
			snap.Status = StatusTimeout
		} else {
			snap.Status = StatusDone
		}
		snap.EndedAt = &event.Ts
		if existing != nil && existing.StartedAt != nil {
			ms := event.Ts - *existing.StartedAt
			snap.RuntimeMs = &ms
		}

	case PhaseError:
		snap.Status = StatusFailed
		snap.EndedAt = &event.Ts
		if existing != nil && existing.StartedAt != nil {
			ms := event.Ts - *existing.StartedAt
			snap.RuntimeMs = &ms
		}
	}

	return snap
}
