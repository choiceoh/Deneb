// Package autonomous implements goal-driven autonomous agent execution.
//
// The autonomous subsystem manages a set of user-defined goals and periodically
// executes decision cycles via the LLM agent to make progress on them. Goals
// are persisted to disk as JSON; cycles are triggered by a timer, external
// webhooks, or goal mutations.
package autonomous

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DefaultAutonomousDir is the default directory for autonomous data.
const DefaultAutonomousDir = ".deneb/autonomous"

// DefaultGoalStorePath returns the default path for the goal store.
func DefaultGoalStorePath(homeDir string) string {
	return filepath.Join(homeDir, DefaultAutonomousDir, "goals.json")
}

// Priority levels for goals.
const (
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
)

// Goal status values.
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusPaused    = "paused"
)

// Goal represents a single autonomous objective.
type Goal struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Priority     string `json:"priority"`              // "high", "medium", "low"
	Status       string `json:"status"`                // "active", "completed", "paused"
	LastNote     string `json:"lastNote,omitempty"`     // progress note from last cycle
	PausedReason string `json:"pausedReason,omitempty"` // why the goal was paused
	CycleCount   int    `json:"cycleCount,omitempty"`   // how many cycles worked on this goal
	CreatedAtMs  int64  `json:"createdAtMs"`
	UpdatedAtMs  int64  `json:"updatedAtMs"`
}

// CompletedGoalRetentionMs is how long completed goals are kept before auto-purge (7 days).
const CompletedGoalRetentionMs = 7 * 24 * 60 * 60 * 1000

// GoalUpdate represents a goal state change parsed from cycle output.
type GoalUpdate struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
	Note   string `json:"note,omitempty"`
}

// CycleState tracks persistent cycle execution state across restarts.
type CycleState struct {
	LastRunAtMs       int64  `json:"lastRunAtMs,omitempty"`
	LastStatus        string `json:"lastStatus,omitempty"`        // "ok", "error", "skipped"
	LastError         string `json:"lastError,omitempty"`
	LastSummary       string `json:"lastSummary,omitempty"`       // short output summary
	ConsecutiveErrors int    `json:"consecutiveErrors,omitempty"`
	TotalCycles       int    `json:"totalCycles,omitempty"`
	TotalErrors       int    `json:"totalErrors,omitempty"`
}

// GoalStoreFile is the on-disk format for the goal store.
type GoalStoreFile struct {
	Version    int        `json:"version"`
	Goals      []Goal     `json:"goals"`
	CycleState CycleState `json:"cycleState,omitempty"`
}

// MaxGoals is the maximum number of goals allowed.
const MaxGoals = 20

// MaxDescriptionLen is the maximum length for a goal description.
const MaxDescriptionLen = 500

// GoalStore manages goal persistence with atomic writes and caching.
type GoalStore struct {
	mu         sync.Mutex
	path       string
	cached     *GoalStoreFile
	cachedJSON string
}

// NewGoalStore creates a new goal store at the given path.
func NewGoalStore(storePath string) *GoalStore {
	return &GoalStore{path: storePath}
}

// Load reads the goal store from disk. Returns an empty store if the file doesn't exist.
func (s *GoalStore) Load() (*GoalStoreFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *GoalStore) loadLocked() (*GoalStoreFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			empty := &GoalStoreFile{Version: 1, Goals: []Goal{}}
			s.cached = empty
			s.cachedJSON = ""
			return empty, nil
		}
		return nil, fmt.Errorf("read goal store: %w", err)
	}

	var store GoalStoreFile
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse goal store at %s: %w", s.path, err)
	}
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Goals == nil {
		store.Goals = []Goal{}
	}

	serialized, _ := json.MarshalIndent(store, "", "  ")
	s.cached = &store
	s.cachedJSON = string(serialized)
	return &store, nil
}

// Save writes the goal store to disk atomically.
func (s *GoalStore) Save(store *GoalStoreFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(store)
}

func (s *GoalStore) saveLocked(store *GoalStoreFile) error {
	serialized, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize goal store: %w", err)
	}
	jsonStr := string(serialized)

	if jsonStr == s.cachedJSON {
		return nil
	}

	storeDir := filepath.Dir(s.path)
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return fmt.Errorf("create goal store dir: %w", err)
	}

	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	tmp := fmt.Sprintf("%s.%d.%s.tmp", s.path, os.Getpid(), hex.EncodeToString(randBytes))

	if err := os.WriteFile(tmp, serialized, 0o600); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write goal store temp: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		if copyErr := copyFile(tmp, s.path); copyErr != nil {
			os.Remove(tmp)
			return fmt.Errorf("rename goal store: %w", err)
		}
		os.Remove(tmp)
	}

	s.cached = store
	s.cachedJSON = jsonStr
	return nil
}

// Add creates a new goal and persists to disk.
func (s *GoalStore) Add(description, priority string) (Goal, error) {
	if description == "" {
		return Goal{}, fmt.Errorf("description is required")
	}
	if len(description) > MaxDescriptionLen {
		return Goal{}, fmt.Errorf("description exceeds %d characters", MaxDescriptionLen)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return Goal{}, err
	}

	// Count active + paused goals (completed goals don't count toward the limit).
	liveCount := 0
	for _, g := range store.Goals {
		if g.Status != StatusCompleted {
			liveCount++
		}
	}
	if liveCount >= MaxGoals {
		return Goal{}, fmt.Errorf("goal limit reached (%d); remove or complete existing goals first", MaxGoals)
	}

	now := time.Now().UnixMilli()
	idBytes := make([]byte, 6)
	rand.Read(idBytes)

	goal := Goal{
		ID:          hex.EncodeToString(idBytes),
		Description: description,
		Priority:    normalizePriority(priority),
		Status:      StatusActive,
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}

	store.Goals = append(store.Goals, goal)
	if err := s.saveLocked(store); err != nil {
		return Goal{}, err
	}
	return goal, nil
}

// UpdateCycleState persists the cycle execution state.
func (s *GoalStore) UpdateCycleState(state CycleState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return err
	}
	store.CycleState = state
	return s.saveLocked(store)
}

// LoadCycleState returns the persisted cycle state.
func (s *GoalStore) LoadCycleState() (CycleState, error) {
	store, err := s.Load()
	if err != nil {
		return CycleState{}, err
	}
	return store.CycleState, nil
}

// Remove deletes a goal by ID and persists.
func (s *GoalStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return err
	}

	filtered := make([]Goal, 0, len(store.Goals))
	found := false
	for _, g := range store.Goals {
		if g.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, g)
	}
	if !found {
		return fmt.Errorf("goal %q not found", id)
	}

	store.Goals = filtered
	return s.saveLocked(store)
}

// List returns all goals, sorted by priority (high > medium > low).
func (s *GoalStore) List() ([]Goal, error) {
	store, err := s.Load()
	if err != nil {
		return nil, err
	}
	goals := make([]Goal, len(store.Goals))
	copy(goals, store.Goals)
	sort.Slice(goals, func(i, j int) bool {
		return priorityRank(goals[i].Priority) > priorityRank(goals[j].Priority)
	})
	return goals, nil
}

// ActiveGoals returns only goals with status "active", sorted by priority.
func (s *GoalStore) ActiveGoals() ([]Goal, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	active := make([]Goal, 0, len(all))
	for _, g := range all {
		if g.Status == StatusActive {
			active = append(active, g)
		}
	}
	return active, nil
}

// Update modifies a goal's status and/or note. Called by the cycle runner
// after parsing LLM output. Tracks cycle count and paused reason.
func (s *GoalStore) Update(id, status, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return err
	}

	for i := range store.Goals {
		if store.Goals[i].ID == id {
			if status != "" {
				store.Goals[i].Status = status
			}
			if note != "" {
				store.Goals[i].LastNote = note
			}
			// Track why a goal was paused.
			if status == StatusPaused && note != "" {
				store.Goals[i].PausedReason = note
			} else if status == StatusActive {
				store.Goals[i].PausedReason = ""
			}
			store.Goals[i].CycleCount++
			store.Goals[i].UpdatedAtMs = time.Now().UnixMilli()
			return s.saveLocked(store)
		}
	}
	return fmt.Errorf("goal %q not found", id)
}

// UpdateGoal patches a goal's priority and/or status. Used by the RPC handler
// for manual goal management (e.g., reactivating a paused goal, changing priority).
func (s *GoalStore) UpdateGoal(id string, priority, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return err
	}

	for i := range store.Goals {
		if store.Goals[i].ID == id {
			if priority != "" {
				store.Goals[i].Priority = normalizePriority(priority)
			}
			if status != "" {
				switch status {
				case StatusActive, StatusCompleted, StatusPaused:
					store.Goals[i].Status = status
					if status == StatusActive {
						store.Goals[i].PausedReason = ""
					}
				default:
					return fmt.Errorf("invalid status %q", status)
				}
			}
			store.Goals[i].UpdatedAtMs = time.Now().UnixMilli()
			return s.saveLocked(store)
		}
	}
	return fmt.Errorf("goal %q not found", id)
}

// PurgeCompleted removes completed goals older than CompletedGoalRetentionMs.
// Returns the number of purged goals.
func (s *GoalStore) PurgeCompleted() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadLocked()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().UnixMilli() - CompletedGoalRetentionMs
	filtered := make([]Goal, 0, len(store.Goals))
	purged := 0
	for _, g := range store.Goals {
		if g.Status == StatusCompleted && g.UpdatedAtMs < cutoff {
			purged++
			continue
		}
		filtered = append(filtered, g)
	}
	if purged == 0 {
		return 0, nil
	}

	store.Goals = filtered
	if err := s.saveLocked(store); err != nil {
		return 0, err
	}
	return purged, nil
}

// RecentlyChanged returns goals whose status changed in the last N milliseconds.
// Used to inject recent completions/pauses into the decision prompt.
func (s *GoalStore) RecentlyChanged(withinMs int64) ([]Goal, error) {
	store, err := s.Load()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UnixMilli() - withinMs
	var recent []Goal
	for _, g := range store.Goals {
		if g.UpdatedAtMs >= cutoff && g.Status != StatusActive {
			recent = append(recent, g)
		}
	}
	return recent, nil
}

func normalizePriority(p string) string {
	switch p {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return p
	default:
		return PriorityMedium
	}
}

func priorityRank(p string) int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
