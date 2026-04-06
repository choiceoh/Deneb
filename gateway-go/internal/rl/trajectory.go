package rl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// Trajectory captures a single local AI call for RL training.
type Trajectory struct {
	ID          string         `json:"id"`
	TaskType    string         `json:"task_type"`
	System      string         `json:"system"`
	UserMessage string         `json:"user_message"`
	Response    string         `json:"response"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CapturedAt  int64          `json:"captured_at"` // unix millis
}

// TrajectoryStats summarizes the store state.
type TrajectoryStats struct {
	Total    int            `json:"total"`
	ByTask   map[string]int `json:"by_task"`
	Exported int64          `json:"exported"`
}

// Store is an in-memory ring buffer for trajectories.
// Thread-safe. When full, oldest entries are overwritten.
type Store struct {
	mu       sync.RWMutex
	ring     []Trajectory
	head     int // next write position
	count    int
	capacity int
	exported int64
}

// NewStore creates a trajectory store with the given capacity.
func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Store{
		ring:     make([]Trajectory, capacity),
		capacity: capacity,
	}
}

// Add appends a trajectory. If the buffer is full, the oldest entry is overwritten.
func (s *Store) Add(t Trajectory) {
	if t.ID == "" {
		t.ID = shortid.New("traj")
	}
	if t.CapturedAt == 0 {
		t.CapturedAt = time.Now().UnixMilli()
	}
	s.mu.Lock()
	s.ring[s.head] = t
	s.head = (s.head + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}
	s.mu.Unlock()
}

// List returns trajectories, optionally filtered by task type.
// Returns newest first, up to limit entries.
func (s *Store) List(taskType string, limit int) []Trajectory {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > s.count {
		limit = s.count
	}

	result := make([]Trajectory, 0, limit)
	// Walk backward from head to get newest first.
	for i := 0; i < s.count && len(result) < limit; i++ {
		idx := (s.head - 1 - i + s.capacity) % s.capacity
		t := s.ring[idx]
		if taskType != "" && t.TaskType != taskType {
			continue
		}
		result = append(result, t)
	}
	return result
}

// Stats returns summary statistics.
func (s *Store) Stats() TrajectoryStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byTask := make(map[string]int)
	for i := 0; i < s.count; i++ {
		idx := (s.head - 1 - i + s.capacity) % s.capacity
		byTask[s.ring[idx].TaskType]++
	}
	return TrajectoryStats{
		Total:    s.count,
		ByTask:   byTask,
		Exported: s.exported,
	}
}

// ExportJSONL writes trajectories to a JSONL file, optionally filtered by task type.
// Returns the file path and count of written entries.
func (s *Store) ExportJSONL(dir string, taskType string) (string, int, error) {
	if dir == "" {
		return "", 0, fmt.Errorf("rl: export directory not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("rl: mkdir %s: %w", dir, err)
	}

	items := s.List(taskType, 0) // all matching
	if len(items) == 0 {
		return "", 0, nil
	}

	suffix := "all"
	if taskType != "" {
		suffix = taskType
	}
	filename := fmt.Sprintf("trajectories_%s_%d.jsonl", suffix, time.Now().UnixMilli())
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return "", 0, fmt.Errorf("rl: create %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	written := 0
	for i := len(items) - 1; i >= 0; i-- { // oldest first for training
		if err := enc.Encode(items[i]); err != nil {
			continue
		}
		written++
	}

	s.mu.Lock()
	s.exported += int64(written)
	s.mu.Unlock()

	return path, written, nil
}
