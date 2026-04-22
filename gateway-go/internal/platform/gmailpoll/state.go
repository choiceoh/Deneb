// Package gmailpoll implements periodic Gmail polling with LLM-based analysis.
// New emails are detected, analyzed via a configurable prompt, and reported
// to the user through Telegram.
package gmailpoll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	maxSeenIDs       = 200
	defaultStateFile = "gmail-poll-state.json"
)

// PollState tracks which emails have been processed across restarts.
type PollState struct {
	LastPollAt int64    `json:"lastPollAt"`
	SeenIDs    []string `json:"seenIds"`

	// seenSet is an in-memory index for O(1) lookups, rebuilt on Load.
	seenSet map[string]struct{} `json:"-"`
}

// stateStore handles persistence of poll state to disk.
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
	// Rebuild the in-memory set from the persisted slice.
	state.seenSet = make(map[string]struct{}, len(state.SeenIDs))
	for _, id := range state.SeenIDs {
		state.seenSet[id] = struct{}{}
	}
	return &state, nil
}

func (s *stateStore) Save(state *PollState) error {
	// Trim SeenIDs to prevent unbounded growth.
	if len(state.SeenIDs) > maxSeenIDs {
		state.SeenIDs = state.SeenIDs[len(state.SeenIDs)-maxSeenIDs:]
		// Rebuild set after trim.
		state.seenSet = make(map[string]struct{}, len(state.SeenIDs))
		for _, id := range state.SeenIDs {
			state.seenSet[id] = struct{}{}
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal poll state: %w", err)
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Atomic write via temp file + rename, with one retry on transient
	// filesystem errors so brief hiccups don't surface as persist failures.
	tmp := s.path + ".tmp"
	_ = os.Remove(tmp) // clean up any leftover tmp from a prior interrupted save
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
	_ = os.Remove(tmp) // best-effort cleanup on final rename failure
	return renameErr
}

// hasSeen checks if a message ID has already been processed (O(1)).
func (state *PollState) hasSeen(id string) bool {
	if state.seenSet == nil {
		return false
	}
	_, ok := state.seenSet[id]
	return ok
}

// markSeen adds a message ID to the seen list and set.
func (state *PollState) markSeen(id string) {
	if state.seenSet == nil {
		state.seenSet = make(map[string]struct{})
	}
	state.SeenIDs = append(state.SeenIDs, id)
	state.seenSet[id] = struct{}{}
}
