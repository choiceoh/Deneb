package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// DefaultCronDir is the default directory for cron data.
const DefaultCronDir = ".deneb/cron"

// DefaultCronStorePath returns the default path for the cron job store.
func DefaultCronStorePath(homeDir string) string {
	return filepath.Join(homeDir, DefaultCronDir, "jobs.json")
}

// CronStoreFile is the on-disk format for the cron job store.
type CronStoreFile struct {
	Version int        `json:"version"`
	Jobs    []StoreJob `json:"jobs"`
}

// StoreJob is the on-disk representation of a cron job with full state.
type StoreJob struct {
	ID            string             `json:"id"`
	Name          string             `json:"name,omitempty"`
	AgentID       string             `json:"agentId,omitempty"`
	Enabled       bool               `json:"enabled"`
	SessionTarget CronSessionTarget  `json:"sessionTarget,omitempty"` // "main", "isolated", "current", "subagent"
	Schedule      StoreSchedule      `json:"schedule"`
	Payload       StorePayload       `json:"payload"`
	Delivery      *JobDeliveryConfig `json:"delivery,omitempty"`
	FailureAlert  *CronFailureAlert  `json:"failureAlert,omitempty"`
	State         JobState           `json:"state"`
	CreatedAtMs   int64              `json:"createdAtMs,omitempty"`
	UpdatedAtMs   int64              `json:"updatedAtMs,omitempty"`
}

// StoreSchedule represents the schedule configuration on disk.
// Supports three kinds: "at" (one-shot), "every" (interval), "cron" (expression).
type StoreSchedule struct {
	Kind      string `json:"kind"`                // "at", "every", "cron"
	At        string `json:"at,omitempty"`        // ISO8601 for kind=at
	EveryMs   int64  `json:"everyMs,omitempty"`   // interval for kind=every
	AnchorMs  int64  `json:"anchorMs,omitempty"`  // anchor point for kind=every
	Expr      string `json:"expr,omitempty"`      // cron expression for kind=cron
	Tz        string `json:"tz,omitempty"`        // timezone for kind=cron
	StaggerMs int64  `json:"staggerMs,omitempty"` // stagger window for kind=cron
}

// StorePayload represents the job payload on disk.
type StorePayload struct {
	Kind           string `json:"kind"`              // "agentTurn" or "systemEvent"
	Message        string `json:"message,omitempty"` // for agentTurn
	Text           string `json:"text,omitempty"`    // for systemEvent
	Model          string `json:"model,omitempty"`
	Thinking       string `json:"thinking,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	LightContext   bool   `json:"lightContext,omitempty"`
}

// JobState tracks runtime state for a cron job.
// Run-level details (status, error, duration, timing) are delegated to session.Manager
// via LastSessionKey; only cron-specific bookkeeping remains here.
type JobState struct {
	NextRunAtMs          int64  `json:"nextRunAtMs,omitempty"`
	LastSessionKey       string `json:"lastSessionKey,omitempty"` // session key for last run (lookup via session.Manager)
	ConsecutiveErrors    int    `json:"consecutiveErrors,omitempty"`
	LastDeliveryStatus   string `json:"lastDeliveryStatus,omitempty"`
	LastDeliveryError    string `json:"lastDeliveryError,omitempty"`
	LastFailureAlertAtMs int64  `json:"lastFailureAlertAtMs,omitempty"`
	ScheduleErrorCount   int    `json:"scheduleErrorCount,omitempty"`
}

// Store manages cron job persistence with atomic writes and caching.
type Store struct {
	mu         sync.Mutex
	path       string
	cached     *CronStoreFile
	cachedJSON string // serialized JSON for diffing
}

// NewStore creates a new cron job store at the given path.
func NewStore(storePath string) *Store {
	return &Store{path: storePath}
}

// Load reads the cron store from disk. Returns an empty store if the file doesn't exist.
func (s *Store) Load() (*CronStoreFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			empty := &CronStoreFile{Version: 1, Jobs: []StoreJob{}}
			s.cached = empty
			s.cachedJSON = ""
			return empty, nil
		}
		return nil, fmt.Errorf("read cron store: %w", err)
	}

	var store CronStoreFile
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse cron store at %s: %w", s.path, err)
	}
	if store.Version == 0 {
		store.Version = 1
	}
	if store.Jobs == nil {
		store.Jobs = []StoreJob{}
	}

	serialized, _ := json.MarshalIndent(store, "", "  ")
	s.cached = &store
	s.cachedJSON = string(serialized)
	return &store, nil
}

// Save writes the cron store to disk atomically with a backup.
// Skips write if the serialized content hasn't changed.
func (s *Store) Save(store *CronStoreFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	serialized, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize cron store: %w", err)
	}
	jsonStr := string(serialized)

	// Skip write if unchanged.
	if jsonStr == s.cachedJSON {
		return nil
	}

	if err := atomicfile.WriteFile(s.path, serialized, &atomicfile.Options{
		Perm:    0o600,
		DirPerm: 0o700,
		Fsync:   true,
		Backup:  s.cachedJSON != "",
	}); err != nil {
		return fmt.Errorf("save cron store: %w", err)
	}

	s.cached = store
	s.cachedJSON = jsonStr
	return nil
}

// GetJob returns a job by ID from the cached store, or nil if not found.
func (s *Store) GetJob(id string) *StoreJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached == nil {
		return nil
	}
	for i := range s.cached.Jobs {
		if s.cached.Jobs[i].ID == id {
			cp := s.cached.Jobs[i]
			return &cp
		}
	}
	return nil
}

// AddJob adds a job to the store and saves to disk.
func (s *Store) AddJob(job StoreJob) error {
	store, err := s.Load()
	if err != nil {
		return err
	}

	// Replace if exists.
	found := false
	for i := range store.Jobs {
		if store.Jobs[i].ID == job.ID {
			store.Jobs[i] = job
			found = true
			break
		}
	}
	if !found {
		store.Jobs = append(store.Jobs, job)
	}
	return s.Save(store)
}

// RemoveJob removes a job by ID and saves to disk.
func (s *Store) RemoveJob(id string) error {
	store, err := s.Load()
	if err != nil {
		return err
	}

	filtered := make([]StoreJob, 0, len(store.Jobs))
	for _, j := range store.Jobs {
		if j.ID != id {
			filtered = append(filtered, j)
		}
	}
	store.Jobs = filtered
	return s.Save(store)
}

// UpdateJobState updates only the state of a job by ID and saves.
func (s *Store) UpdateJobState(id string, state JobState) error {
	store, err := s.Load()
	if err != nil {
		return err
	}
	for i := range store.Jobs {
		if store.Jobs[i].ID == id {
			store.Jobs[i].State = state
			return s.Save(store)
		}
	}
	return fmt.Errorf("job %q not found", id)
}
