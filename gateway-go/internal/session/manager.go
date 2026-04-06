// Package session manages gateway session lifecycle.
//
// Sessions follow a state machine: IDLE → RUNNING → {DONE | FAILED | KILLED | TIMEOUT}.
// The Manager tracks sessions in memory and emits events on state transitions.
// A background GC goroutine evicts terminal sessions after gcMaxAge (1 hour).
package session

import (
	"context"
	"sync"
	"time"
)

// RunStatus mirrors SessionRunStatus from TypeScript.
type RunStatus string

const (
	StatusRunning RunStatus = "running"
	StatusDone    RunStatus = "done"
	StatusFailed  RunStatus = "failed"
	StatusKilled  RunStatus = "killed"
	StatusTimeout RunStatus = "timeout"
)

// Kind mirrors the TypeScript session kind union.
type Kind string

const (
	KindDirect   Kind = "direct"
	KindGroup    Kind = "group"
	KindGlobal   Kind = "global"
	KindUnknown  Kind = "unknown"
	KindCron     Kind = "cron"
	KindSubagent Kind = "subagent"
)

// SessionMode controls the behavioral mode of a session.
// Each mode gates different capabilities (tool sets, autonomous continuation, etc.).
type SessionMode string

const (
	// ModeNormal is the default mode — full tools, autonomous continuation enabled.
	ModeNormal SessionMode = ""
	// ModeChat restricts tools to conversation-only (web search, etc.), no continuation.
	ModeChat SessionMode = "chat"
	// ModeWork enables extended agent behavior (higher continuation limits).
	ModeWork SessionMode = "work"
)

// IsInternal returns true for session kinds that are system-internal
// (cron, subagent) and should be excluded from user-facing listings.
func (k Kind) IsInternal() bool {
	switch k {
	case KindCron, KindSubagent:
		return true
	default:
		return false
	}
}

// ModelConfig holds inference and model-selection settings for a session.
// Fields are embedded into Session and serialize as flat JSON keys.
type ModelConfig struct {
	// Inference mode controls.
	ThinkingLevel  string `json:"thinkingLevel,omitempty"`
	FastMode       *bool  `json:"fastMode,omitempty"`
	VerboseLevel   string `json:"verboseLevel,omitempty"`
	ReasoningLevel string `json:"reasoningLevel,omitempty"`
	ElevatedLevel  string `json:"elevatedLevel,omitempty"`
	ResponseUsage  string `json:"responseUsage,omitempty"`

	// Per-mode model overrides (empty → use session default).
	ThinkingModel  string `json:"thinkingModel,omitempty"`
	FastModel      string `json:"fastModel,omitempty"`
	ReasoningModel string `json:"reasoningModel,omitempty"`
}

// ExecConfig holds execution environment settings for a session.
// Fields are embedded into Session and serialize as flat JSON keys.
type ExecConfig struct {
	ExecHost     string `json:"execHost,omitempty"`
	ExecSecurity string `json:"execSecurity,omitempty"`
	ExecAsk      string `json:"execAsk,omitempty"`
	ExecNode     string `json:"execNode,omitempty"`
}

// AgentConfig holds spawn lineage and messaging policy for a session.
// Fields are embedded into Session and serialize as flat JSON keys.
type AgentConfig struct {
	// Spawn / subagent lineage.
	SpawnedBy            string `json:"spawnedBy,omitempty"`
	SpawnedWorkspaceDir  string `json:"spawnedWorkspaceDir,omitempty"`
	SpawnDepth           *int   `json:"spawnDepth,omitempty"`
	SubagentRole         string `json:"subagentRole,omitempty"`
	SubagentControlScope string `json:"subagentControlScope,omitempty"`

	// Tool restriction.
	ToolPreset string `json:"toolPreset,omitempty"` // researcher, implementer, verifier, coordinator

	// Channel / messaging policy.
	SendPolicy      string `json:"sendPolicy,omitempty"`
	GroupActivation string `json:"groupActivation,omitempty"`
}

// Session represents a gateway session row.
// Configuration fields are grouped into embedded structs (ModelConfig,
// ExecConfig, AgentConfig) for readability; they remain flat in JSON.
type Session struct {
	// Core identity and lifecycle.
	Key            string      `json:"key"`
	Kind           Kind        `json:"kind"`
	Mode           SessionMode `json:"mode,omitempty"`
	Status         RunStatus   `json:"status,omitempty"`
	Channel        string    `json:"channel,omitempty"`
	Model          string    `json:"model,omitempty"`
	UpdatedAt      int64     `json:"updatedAt,omitempty"`
	StartedAt      *int64    `json:"startedAt,omitempty"`
	EndedAt        *int64    `json:"endedAt,omitempty"`
	RuntimeMs      *int64    `json:"runtimeMs,omitempty"`
	AbortedLastRun bool      `json:"abortedLastRun"`
	CreatedAt      time.Time `json:"-"`
	SessionID      string    `json:"sessionId,omitempty"`
	Label          string    `json:"label,omitempty"`

	// Token accounting (cleared on compaction).
	InputTokens  *int64 `json:"inputTokens,omitempty"`
	OutputTokens *int64 `json:"outputTokens,omitempty"`
	TotalTokens  *int64 `json:"totalTokens,omitempty"`

	// TimeoutAt is the absolute timestamp (UnixMilli) when a running session
	// should be forcibly transitioned to StatusTimeout. Used for subagent
	// sessions to prevent indefinitely hung agents. Zero means no timeout.
	TimeoutAt *int64 `json:"timeoutAt,omitempty"`

	// IdleTimeoutMs is the maximum duration (in milliseconds) a session can
	// remain running without activity before being transitioned to StatusTimeout.
	// Zero means no idle timeout. Used for subagent stall detection.
	IdleTimeoutMs int64 `json:"idleTimeoutMs,omitempty"`

	// LastActivityAt is the timestamp (UnixMilli) of the last meaningful
	// activity in this session (tool execution, output produced, etc.).
	// Updated via Manager.TouchActivity(). Used for idle stall detection.
	LastActivityAt *int64 `json:"lastActivityAt,omitempty"`

	// LastOutput stores the last assistant output text for the session.
	// Used by cron runner to retrieve the agent's response after completion.
	LastOutput string `json:"lastOutput,omitempty"`

	// FailureReason holds a human-readable description of why the last run failed.
	// Cleared when the session transitions back to Running (successful retry).
	FailureReason string `json:"failureReason,omitempty"`

	// Grouped configuration (embedded; JSON keys remain flat).
	ModelConfig
	ExecConfig
	AgentConfig
}

// Session GC constants.
const (
	// gcInterval is how often the GC scans for stale sessions.
	gcInterval = 10 * time.Minute
	// gcMaxAge is the default retention for terminal sessions.
	gcMaxAge = 1 * time.Hour
)

// gcMaxAgeForKind returns the retention period for terminal sessions of a given kind.
// Cron sessions are retained longer since they serve as audit trail.
func gcMaxAgeForKind(k Kind) time.Duration {
	switch k {
	case KindCron:
		return 24 * time.Hour
	case KindSubagent:
		return 2 * time.Hour
	default:
		return gcMaxAge
	}
}

// Manager tracks active sessions in memory.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	eventBus *EventBus
	gcOnce   sync.Once // ensures StartGC spawns at most one GC goroutine

	// emitMu serializes state-mutation + event-emission as an atomic unit.
	// Without this, a gap between mu.Unlock() and eventBus.Emit() allows
	// concurrent mutations to interleave, causing out-of-order events
	// (e.g., EventDeleted arriving after a concurrent EventCreated for the
	// same key).
	//
	// Lock ordering: emitMu → mu (never acquire mu then emitMu).
	// Event subscribers must NOT call mutating Manager methods (Set, Delete,
	// Create, Patch, etc.) or they will deadlock on emitMu re-entry.
	emitMu sync.Mutex
}

// NewManager creates an empty session manager with an integrated event bus.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		eventBus: NewEventBus(),
	}
}

// StartGC starts a background goroutine that periodically evicts terminal
// sessions (done/failed/killed/timeout) older than gcMaxAge.
// Stops when ctx is canceled. Safe to call multiple times — only the first
// call starts a goroutine; subsequent calls are no-ops.
func (m *Manager) StartGC(ctx context.Context) {
	m.gcOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(gcInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					m.evictStale()
				}
			}
		}()
	})
}

// evictStale removes terminal sessions whose UpdatedAt is older than the
// Kind-specific retention period, and enforces TimeoutAt on running sessions.
func (m *Manager) evictStale() {
	now := time.Now()
	nowMs := now.UnixMilli()
	var evicted []string
	var timedOut []string

	m.mu.Lock()
	for key, s := range m.sessions {
		if isTerminal(s.Status) {
			maxAge := gcMaxAgeForKind(s.Kind)
			if s.UpdatedAt < now.Add(-maxAge).UnixMilli() {
				delete(m.sessions, key)
				evicted = append(evicted, key)
			}
		} else if s.Status == StatusRunning && s.TimeoutAt != nil && nowMs > *s.TimeoutAt {
			s.Status = StatusTimeout
			endedAt := nowMs
			s.EndedAt = &endedAt
			s.UpdatedAt = nowMs
			timedOut = append(timedOut, key)
		} else if s.Status == StatusRunning && s.IdleTimeoutMs > 0 {
			// Idle stall detection: if no activity for IdleTimeoutMs, timeout.
			activityAt := s.UpdatedAt
			if s.LastActivityAt != nil {
				activityAt = *s.LastActivityAt
			}
			if nowMs-activityAt > s.IdleTimeoutMs {
				s.Status = StatusTimeout
				endedAt := nowMs
				s.EndedAt = &endedAt
				s.UpdatedAt = nowMs
				s.FailureReason = "idle timeout: no activity detected"
				timedOut = append(timedOut, key)
			}
		}
	}
	m.mu.Unlock()

	if len(evicted) == 0 && len(timedOut) == 0 {
		return
	}

	// Hold emitMu only for the event emission phase so concurrent
	// mutations cannot interleave between deletion and EventDeleted.
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

	for _, key := range evicted {
		m.eventBus.Emit(Event{Kind: EventDeleted, Key: key})
	}
	for _, key := range timedOut {
		m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: key, OldStatus: StatusRunning, NewStatus: StatusTimeout})
	}
}

// isTerminal returns true for session statuses that represent completed runs.
func isTerminal(s RunStatus) bool {
	switch s {
	case StatusDone, StatusFailed, StatusKilled, StatusTimeout:
		return true
	}
	return false
}

// EventBusRef returns the session event bus for subscribing to lifecycle events.
func (m *Manager) EventBusRef() *EventBus {
	return m.eventBus
}

// Get returns a snapshot copy of a session by key, or nil if not found.
// The returned Session is safe to read without holding the manager lock.
func (m *Manager) Get(key string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.sessions[key]
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// Set stores or updates a session. Ignores sessions with empty keys.
// Returns an error if the status transition is invalid for an existing session.
// Transitions are only enforced when both the old and new statuses are non-empty,
// so callers may bootstrap a session to any initial status via a fresh Set.
func (m *Manager) Set(s *Session) error {
	if s == nil || s.Key == "" {
		return nil
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

	m.mu.Lock()
	old := m.sessions[s.Key]
	var oldStatus RunStatus
	if old != nil {
		oldStatus = old.Status
	}
	newStatus := s.Status
	// Validate status transition for existing sessions with known statuses.
	if old != nil && oldStatus != "" && newStatus != "" && oldStatus != newStatus {
		if err := ValidateTransition(oldStatus, newStatus); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.sessions[s.Key] = s
	m.mu.Unlock()

	if old == nil {
		m.eventBus.Emit(Event{Kind: EventCreated, Key: s.Key})
	} else if oldStatus != newStatus {
		m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: s.Key, OldStatus: oldStatus, NewStatus: newStatus})
	}
	return nil
}

// Delete removes a session by key. Returns true if the session existed.
func (m *Manager) Delete(key string) bool {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

	m.mu.Lock()
	s := m.sessions[key]
	ok := s != nil
	var oldStatus RunStatus
	if s != nil {
		oldStatus = s.Status
	}
	delete(m.sessions, key)
	m.mu.Unlock()

	if ok {
		m.eventBus.Emit(Event{Kind: EventDeleted, Key: key, OldStatus: oldStatus})
	}
	return ok
}

// TouchActivity updates the LastActivityAt timestamp for a session,
// used for idle stall detection. No-op if the session doesn't exist or
// is not running. This is a lightweight, lock-minimized operation.
func (m *Manager) TouchActivity(key string) {
	now := time.Now().UnixMilli()
	m.mu.Lock()
	s := m.sessions[key]
	if s != nil && s.Status == StatusRunning {
		s.LastActivityAt = &now
	}
	m.mu.Unlock()
}

// List returns snapshot copies of all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		cp := *s
		result = append(result, &cp)
	}
	return result
}

// replaySet stores a session without state machine validation or event emission.
// Used only during WAL replay where entries were already validated at write time.
func (m *Manager) replaySet(s *Session) {
	if s == nil || s.Key == "" {
		return
	}
	m.mu.Lock()
	m.sessions[s.Key] = s
	m.mu.Unlock()
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Create creates a new session with the given key and kind.
// Returns a snapshot copy safe for concurrent use.
// Returns nil if key is empty.
func (m *Manager) Create(key string, kind Kind) *Session {
	if key == "" {
		return nil
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

	m.mu.Lock()
	now := time.Now()
	s := &Session{
		Key:       key,
		Kind:      kind,
		UpdatedAt: now.UnixMilli(),
		CreatedAt: now,
	}
	m.sessions[key] = s
	cp := *s
	m.mu.Unlock()

	m.eventBus.Emit(Event{Kind: EventCreated, Key: key})
	return &cp
}

// ApplyLifecycleEvent applies a lifecycle event to a session, creating it if needed.
// Returns a snapshot copy safe for concurrent use.
//
// Behavior by phase:
//   - Start: creates session if absent, sets status to Running.
//   - End/Error: updates terminal status, preserves StartedAt from existing session.
//   - Unknown phase: no-op — returns existing session or a KindUnknown stub so
//     callers can always dereference the result safely without nil checks.
func (m *Manager) ApplyLifecycleEvent(key string, event LifecycleEvent) *Session {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()

	m.mu.Lock()

	existing := m.sessions[key]
	snap := DeriveLifecycleSnapshot(existing, event)

	// Empty snapshot means unknown phase — no-op.
	if snap.Status == "" {
		if existing != nil {
			cp := *existing
			m.mu.Unlock()
			return &cp
		}
		m.mu.Unlock()
		return &Session{Key: key, Kind: KindUnknown}
	}

	// Capture old status before mutation.
	oldStatus := RunStatus("")
	if existing != nil {
		oldStatus = existing.Status
	}

	if existing == nil {
		existing = &Session{Key: key, Kind: KindUnknown, CreatedAt: time.Now()}
		m.sessions[key] = existing
	}

	existing.Status = snap.Status
	existing.AbortedLastRun = snap.AbortedLastRun

	// Prefer snapshot-derived UpdatedAt; fall back to now.
	if snap.UpdatedAt != nil {
		existing.UpdatedAt = *snap.UpdatedAt
	} else {
		existing.UpdatedAt = time.Now().UnixMilli()
	}

	if snap.Status == StatusRunning {
		// Start phase: set StartedAt, clear terminal fields and stale failure reason.
		existing.StartedAt = snap.StartedAt
		existing.EndedAt = nil
		existing.RuntimeMs = nil
		existing.FailureReason = ""
	} else {
		// End/Error phase: preserve existing StartedAt if snapshot doesn't set one.
		if snap.StartedAt != nil {
			existing.StartedAt = snap.StartedAt
		}
		existing.EndedAt = snap.EndedAt
		existing.RuntimeMs = snap.RuntimeMs
		if snap.FailureReason != "" {
			existing.FailureReason = snap.FailureReason
		}
	}

	newStatus := existing.Status
	cp := *existing
	m.mu.Unlock()

	m.eventBus.Emit(Event{Kind: EventStatusChanged, Key: key, OldStatus: oldStatus, NewStatus: newStatus})
	return &cp
}
