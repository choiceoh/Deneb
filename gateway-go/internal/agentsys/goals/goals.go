// Package goals implements persistent cross-turn "standing goals" — the Ralph
// loop. A goal is a free-form objective that survives across agent runs: after
// each run a cheap judge decides whether the goal is satisfied, and if not the
// loop drives another run toward it, until done, budget-exhausted, or the user
// stops it. This package owns only the goal STATE and its persistence; the
// driving loop (idle-gated re-injection + judge) lives in the server layer
// (goal_task.go), and the user-facing /goal command in the chat layer.
//
// Design (ported from Hermes Agent's hermes_cli/goals.py, adapted for Deneb's
// single-user, JSON-persisted, prompt-cache-strict environment):
//   - One standing goal per session, keyed by sessionKey.
//   - Budget = MaxTurns runs; exhaustion auto-pauses (resumable), never clears.
//   - An idempotency LEDGER of destructive action keys committed by completed
//     runs, so a later run never repeats the same email send / file write.
//   - JSON persistence under the state dir (mirrors autonomous_state.json), so
//     a standing goal survives the SIGUSR1 deploy restarts.
package goals

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle state of a standing goal.
type Status string

const (
	// StatusActive — the loop should keep driving runs toward the goal.
	StatusActive Status = "active"
	// StatusPaused — budget exhausted or the judge kept failing; resumable.
	StatusPaused Status = "paused"
	// StatusDone — the judge confirmed completion (or "blocked/needs-input",
	// which is treated as done so the loop stops cleanly).
	StatusDone Status = "done"
	// StatusCleared — the user stopped the goal; kept for audit, not driven.
	StatusCleared Status = "cleared"
)

// DefaultMaxTurns bounds how many runs one goal may consume before auto-pause.
// Mirrors Hermes DEFAULT_MAX_TURNS. Conservative for a single-user, local-cost
// deployment where each run holds the vLLM slot for up to the turn deadline.
const DefaultMaxTurns = 20

// MaxConsecutiveParseFailures auto-pauses a goal after this many runs whose
// judge output could not be parsed — guards against a weak lightweight model
// silently burning the whole budget on un-judgeable verdicts.
const MaxConsecutiveParseFailures = 3

// State is the full persisted state of one standing goal.
type State struct {
	Goal       string `json:"goal"`
	Status     Status `json:"status"`
	SessionKey string `json:"sessionKey"`
	TurnsUsed  int    `json:"turnsUsed"`
	MaxTurns   int    `json:"maxTurns"`
	CreatedAt  int64  `json:"createdAt"` // unix millis
	LastRunAt  int64  `json:"lastRunAt"` // unix millis

	LastVerdict  string `json:"lastVerdict,omitempty"` // "done" | "continue" | "skipped"
	LastReason   string `json:"lastReason,omitempty"`  // judge's one-line rationale
	PausedReason string `json:"pausedReason,omitempty"`

	ConsecParseFailures int `json:"consecParseFailures,omitempty"`

	// Subgoals are completion criteria added (by the agent or user) after the
	// goal was set. When present, the judge requires per-criterion evidence
	// before marking the goal done, and the continuation lists them each run.
	Subgoals []string `json:"subgoals,omitempty"`

	// ExecutedActions is the idempotency ledger: keys of destructive tool
	// actions already committed by completed runs of THIS goal. The driver's
	// before-tool guard blocks any action whose key is already here, so a
	// re-driven run cannot double-send a message or re-run a mutation.
	ExecutedActions map[string]bool `json:"executedActions,omitempty"`
}

// clone returns a deep-ish copy safe to hand outside the lock (the ledger map
// is copied so callers can't mutate the stored map).
func (s *State) clone() *State {
	if s == nil {
		return nil
	}
	cp := *s
	if s.ExecutedActions != nil {
		cp.ExecutedActions = make(map[string]bool, len(s.ExecutedActions))
		for k, v := range s.ExecutedActions {
			cp.ExecutedActions[k] = v
		}
	}
	if s.Subgoals != nil {
		cp.Subgoals = append([]string(nil), s.Subgoals...)
	}
	return &cp
}

// Active reports whether the goal should be driven by the loop.
func (s *State) Active() bool { return s != nil && s.Status == StatusActive }

// Remaining returns how many runs are left in the budget (never negative).
func (s *State) Remaining() int {
	if s == nil {
		return 0
	}
	if r := s.MaxTurns - s.TurnsUsed; r > 0 {
		return r
	}
	return 0
}

// Store persists standing goals (one per session) to a JSON file. Safe for
// concurrent use. A zero-value path disables persistence (in-memory only),
// which is what tests use.
type Store struct {
	mu     sync.Mutex
	path   string // "" = in-memory only
	byKey  map[string]*State
	logger *slog.Logger
	now    func() time.Time // injectable clock for tests
}

// NewStore creates a goal store. stateDir is the directory holding goals.json
// (typically ~/.deneb); an empty stateDir disables persistence. A missing file
// is normal on first boot.
func NewStore(stateDir string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Store{
		byKey:  make(map[string]*State),
		logger: logger.With("pkg", "goals"),
		now:    time.Now,
	}
	if stateDir != "" {
		s.path = filepath.Join(stateDir, "goals.json")
		s.load()
	}
	return s
}

// load restores persisted goals. Caller must NOT hold the lock.
func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("goals: failed to read state file", "error", err)
		}
		return
	}
	var persisted map[string]*State
	if err := json.Unmarshal(data, &persisted); err != nil {
		s.logger.Warn("goals: failed to parse state file", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range persisted {
		if v != nil {
			s.byKey[k] = v
		}
	}
}

// saveLocked persists the current map. Best-effort: a write failure is logged,
// never fatal. Caller must hold s.mu (it snapshots under the lock then writes).
func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.byKey) //nolint:gosec // G117 — "sessionKey" is a session identifier (e.g. "client:main"), not a secret
	if err != nil {
		s.logger.Warn("goals: failed to marshal state", "error", err)
		return
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		s.logger.Warn("goals: failed to write state file", "error", err)
	}
}

// Set installs a new active goal for the session, resetting the budget and
// ledger. Returns a copy of the new state. maxTurns <= 0 uses DefaultMaxTurns.
func (s *Store) Set(sessionKey, goal string, maxTurns int) *State {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UnixMilli()
	st := &State{
		Goal:            goal,
		Status:          StatusActive,
		SessionKey:      sessionKey,
		TurnsUsed:       0,
		MaxTurns:        maxTurns,
		CreatedAt:       now,
		ExecutedActions: make(map[string]bool),
	}
	s.byKey[sessionKey] = st
	s.saveLocked()
	return st.clone()
}

// Get returns a copy of the session's goal, or nil if none.
func (s *Store) Get(sessionKey string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byKey[sessionKey].clone()
}

// ListActive returns copies of every active goal (those the loop should drive).
func (s *Store) ListActive() []*State {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*State
	for _, st := range s.byKey {
		if st.Active() {
			out = append(out, st.clone())
		}
	}
	return out
}

// RecordRun books one consumed run against the budget and stores the judge's
// verdict, then applies the auto-pause backstops (budget exhaustion and the
// consecutive-parse-failure cap). It returns a copy of the updated state.
//
// verdict is "done" | "continue" | "skipped"; parseFailed indicates the judge
// output could not be parsed (distinct from a transient judge error, which the
// caller treats as "continue" and passes parseFailed=false — fail-open).
func (s *Store) RecordRun(sessionKey, verdict, reason string, parseFailed bool) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil {
		return nil
	}
	st.TurnsUsed++
	st.LastRunAt = s.now().UnixMilli()
	st.LastVerdict = verdict
	st.LastReason = reason

	switch {
	case verdict == "done":
		st.Status = StatusDone
		st.ConsecParseFailures = 0
	case parseFailed:
		st.ConsecParseFailures++
		if st.ConsecParseFailures >= MaxConsecutiveParseFailures {
			st.Status = StatusPaused
			st.PausedReason = "judge 출력을 연속으로 해석하지 못했습니다 — judge 모델을 점검하고 /goal resume 하세요."
		}
	default: // continue
		st.ConsecParseFailures = 0
	}

	// Budget backstop runs after the verdict so a final successful run still
	// completes the goal even if it was the last in the budget.
	if st.Status == StatusActive && st.TurnsUsed >= st.MaxTurns {
		st.Status = StatusPaused
		st.PausedReason = "턴 예산 소진 (" + itoa(st.TurnsUsed) + "/" + itoa(st.MaxTurns) + ") — /goal resume 으로 이어서 진행할 수 있습니다."
	}
	s.saveLocked()
	return st.clone()
}

// SeenAction reports whether a destructive action key was already committed by
// a prior completed run of the session's goal.
func (s *Store) SeenAction(sessionKey, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil || st.ExecutedActions == nil {
		return false
	}
	return st.ExecutedActions[key]
}

// CommitActions records destructive action keys into the session goal's ledger
// so future runs cannot repeat them. No-op if the goal is gone.
func (s *Store) CommitActions(sessionKey string, keys []string) {
	if len(keys) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil {
		return
	}
	if st.ExecutedActions == nil {
		st.ExecutedActions = make(map[string]bool, len(keys))
	}
	for _, k := range keys {
		st.ExecutedActions[k] = true
	}
	s.saveLocked()
}

// AddSubgoal appends a completion criterion to the session's goal and returns a
// copy of the updated state. No-op (nil) if no goal exists; empty text is
// ignored. Subgoals take effect at the next run boundary (judge + continuation).
func (s *Store) AddSubgoal(sessionKey, text string) *State {
	text = strings.TrimSpace(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil {
		return nil
	}
	if text != "" {
		st.Subgoals = append(st.Subgoals, text)
		s.saveLocked()
	}
	return st.clone()
}

// Summary renders a human-readable status block for the goal (Korean). Shared
// by the /goal command and the goal agent tool so both report identically.
func (s *State) Summary() string {
	if s == nil {
		return "진행 중인 표준 목표가 없습니다."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🎯 표준 목표: %s\n", s.Goal)
	fmt.Fprintf(&b, "상태: %s · 진행 %d/%d 단계", statusLabel(s.Status), s.TurnsUsed, s.MaxTurns)
	if len(s.Subgoals) > 0 {
		b.WriteString("\n서브골:")
		for i, sg := range s.Subgoals {
			fmt.Fprintf(&b, "\n  %d. %s", i+1, sg)
		}
	}
	if s.LastVerdict != "" && s.LastReason != "" {
		fmt.Fprintf(&b, "\n최근 판정: %s — %s", s.LastVerdict, s.LastReason)
	}
	if s.PausedReason != "" {
		fmt.Fprintf(&b, "\n일시중지 사유: %s", s.PausedReason)
	}
	return b.String()
}

// statusLabel maps a status to its Korean label.
func statusLabel(st Status) string {
	switch st {
	case StatusActive:
		return "진행중"
	case StatusPaused:
		return "일시중지"
	case StatusDone:
		return "완료"
	case StatusCleared:
		return "중단됨"
	default:
		return string(st)
	}
}

// Pause suspends an active goal (resumable). No-op if not active.
func (s *Store) Pause(sessionKey, reason string) *State {
	return s.setStatus(sessionKey, StatusPaused, reason, false)
}

// Resume reactivates a paused goal and resets the budget (a fresh allotment),
// matching Hermes resume semantics. No-op unless currently paused.
func (s *Store) Resume(sessionKey string) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil || st.Status != StatusPaused {
		return st.clone()
	}
	st.Status = StatusActive
	st.PausedReason = ""
	st.TurnsUsed = 0
	st.ConsecParseFailures = 0
	s.saveLocked()
	return st.clone()
}

// Clear stops a goal (soft: flips to cleared, preserved for audit).
func (s *Store) Clear(sessionKey string) *State {
	return s.setStatus(sessionKey, StatusCleared, "", true)
}

// setStatus transitions a goal's status. resetReason clears PausedReason.
func (s *Store) setStatus(sessionKey string, status Status, reason string, resetReason bool) *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byKey[sessionKey]
	if st == nil {
		return nil
	}
	st.Status = status
	if resetReason {
		st.PausedReason = ""
	} else if reason != "" {
		st.PausedReason = reason
	}
	s.saveLocked()
	return st.clone()
}

// itoa is a tiny dependency-free int formatter for budget messages.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
