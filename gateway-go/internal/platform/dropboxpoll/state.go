// Package dropboxpoll watches a Dropbox folder for new files and triggers an
// agent turn to analyze them, mirroring the gmailpoll service. Detection only:
// the actual OCR/extraction/analysis runs in the chat pipeline via the dropbox
// and wiki tools (those extractors live in the tools package, which platform/*
// cannot import), so this service stays a thin detect-and-trigger loop.
package dropboxpoll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	maxSeenIDs       = 500
	defaultStateFile = "dropbox-poll-state.json"
)

// PollState tracks the watch cursor and processed file IDs across restarts.
type PollState struct {
	Cursor     string   `json:"cursor"`
	LastPollAt int64    `json:"lastPollAt"`
	SeenIDs    []string `json:"seenIds"`

	// seenSet is an in-memory index for O(1) lookups, rebuilt on Load.
	seenSet map[string]struct{} `json:"-"`
}

// stateStore persists poll state to disk.
type stateStore struct {
	path string
}

func newStateStore(stateDir string) *stateStore {
	return &stateStore{path: filepath.Join(stateDir, defaultStateFile)}
}

func (s *stateStore) Load() (*PollState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PollState{seenSet: make(map[string]struct{})}, nil
		}
		return nil, fmt.Errorf("read poll state: %w", err)
	}
	var state PollState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse poll state: %w", err)
	}
	state.seenSet = make(map[string]struct{}, len(state.SeenIDs))
	for _, id := range state.SeenIDs {
		state.seenSet[id] = struct{}{}
	}
	return &state, nil
}

func (s *stateStore) Save(state *PollState) error {
	if len(state.SeenIDs) > maxSeenIDs {
		state.SeenIDs = state.SeenIDs[len(state.SeenIDs)-maxSeenIDs:]
		state.seenSet = make(map[string]struct{}, len(state.SeenIDs))
		for _, id := range state.SeenIDs {
			state.seenSet[id] = struct{}{}
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal poll state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Atomic write via temp file + rename, one retry on transient errors.
	tmp := s.path + ".tmp"
	_ = os.Remove(tmp)
	const maxAttempts = 2
	var writeErr error
	for attempt := range maxAttempts {
		writeErr = os.WriteFile(tmp, data, 0o600)
		if writeErr == nil {
			break
		}
		if attempt+1 < maxAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if writeErr != nil {
		return fmt.Errorf("write temp state: %w", writeErr)
	}

	var renameErr error
	for attempt := range maxAttempts {
		renameErr = os.Rename(tmp, s.path)
		if renameErr == nil {
			return nil
		}
		if attempt+1 < maxAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = os.Remove(tmp)
	return renameErr
}

func (state *PollState) hasSeen(id string) bool {
	if state.seenSet == nil {
		return false
	}
	_, ok := state.seenSet[id]
	return ok
}

func (state *PollState) markSeen(id string) {
	if state.seenSet == nil {
		state.seenSet = make(map[string]struct{})
	}
	state.SeenIDs = append(state.SeenIDs, id)
	state.seenSet[id] = struct{}{}
}
