package rl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxRetries = 3 // drop trajectories after this many failed sends

// Trajectory is a collected session outcome in Atropos-compatible format.
type Trajectory struct {
	// ID uniquely identifies this trajectory (session key).
	ID string `json:"id"`
	// Prompt is the initial user message.
	Prompt string `json:"prompt"`
	// Response is the final assistant output.
	Response string `json:"response"`
	// ToolCalls records tool invocations and their outcomes.
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"`
	// Turns is the number of agent turns in the session.
	Turns int `json:"turns"`
	// Environment identifies the scoring environment.
	Environment string `json:"environment"`
	// CollectedAt is the timestamp of collection (unix ms).
	CollectedAt int64 `json:"collected_at"`
	// retries tracks how many times this trajectory failed to send.
	retries int
}

// ToolCallRecord is a single tool invocation record.
type ToolCallRecord struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// TrajectoryStore collects session trajectories and feeds them to Atropos.
type TrajectoryStore struct {
	mu      sync.Mutex
	pending []*Trajectory
	dataDir string
}

// NewTrajectoryStore creates a new trajectory store.
func NewTrajectoryStore() *TrajectoryStore {
	dataDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		dataDir = filepath.Join(home, ".deneb", "rl", "trajectories")
	}
	return &TrajectoryStore{dataDir: dataDir}
}

// Collect adds a trajectory.
func (s *TrajectoryStore) Collect(t *Trajectory) {
	if t == nil {
		return
	}
	if t.CollectedAt == 0 {
		t.CollectedAt = time.Now().UnixMilli()
	}
	if t.Environment == "" {
		t.Environment = "korean_quality"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, t)
}

// PendingCount returns the number of unflushed trajectories.
func (s *TrajectoryStore) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// FeedToAtropos sends pending trajectories to the Atropos HTTP API.
func (s *TrajectoryStore) FeedToAtropos(ctx context.Context, atroposURL string) (int, error) {
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()

	if len(batch) == 0 {
		return 0, nil
	}

	sent := 0
	client := &http.Client{Timeout: 10 * time.Second}
	for _, t := range batch {
		body, err := json.Marshal(t)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "POST", atroposURL+"/trajectory", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			// Re-queue with retry limit.
			t.retries++
			if t.retries < maxRetries {
				s.mu.Lock()
				s.pending = append(s.pending, t)
				s.mu.Unlock()
			}
			continue
		}
		resp.Body.Close()
		sent++
	}
	return sent, nil
}

// BackupToDisk writes pending trajectories to a JSONL file and clears them.
func (s *TrajectoryStore) BackupToDisk() error {
	s.mu.Lock()
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()

	if s.dataDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}

	filename := fmt.Sprintf("backup_%d.jsonl", time.Now().UnixMilli())
	path := filepath.Join(s.dataDir, filename)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, t := range batch {
		enc.Encode(t)
	}
	return nil
}
