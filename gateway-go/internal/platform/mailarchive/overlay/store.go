// Package overlay persists local Gmail-like state for immutable archive messages.
package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

var statePathLocks = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{locks: make(map[string]*sync.Mutex)}

// MessageState is the local mutation overlay for one read-only archive message.
// The LMTP/IMAP archive remains immutable; native app actions are reflected by
// this small sidecar record.
type MessageState struct {
	Read        bool   `json:"read,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
	Trashed     bool   `json:"trashed,omitempty"`
	Mailbox     string `json:"mailbox,omitempty"`
	UID         string `json:"uid,omitempty"`
	UpdatedAtMS int64  `json:"updatedAtMs,omitempty"`
}

type diskState struct {
	Messages map[string]MessageState `json:"messages"`
}

// Store persists archive locators plus local Gmail-like mutations.
type Store struct {
	path       string
	instanceMu sync.Mutex
	pathMu     *sync.Mutex
}

// NewStore constructs a native mail-state overlay backed by path.
func NewStore(path string) *Store {
	return &Store{path: path, pathMu: sharedStatePathLock(path)}
}

func sharedStatePathLock(path string) *sync.Mutex {
	if path == "" {
		return &sync.Mutex{}
	}
	key := filepath.Clean(path)
	if absolute, err := filepath.Abs(key); err == nil {
		key = absolute
	}
	statePathLocks.Lock()
	defer statePathLocks.Unlock()
	if lock := statePathLocks.locks[key]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	statePathLocks.locks[key] = lock
	return lock
}

func (s *Store) mutex() *sync.Mutex {
	if s.pathMu != nil {
		return s.pathMu
	}
	return &s.instanceMu
}

// Get returns the overlay state for id, or its zero value on miss.
func (s *Store) Get(id string) MessageState {
	if s == nil || id == "" {
		return MessageState{}
	}
	mu := s.mutex()
	mu.Lock()
	defer mu.Unlock()
	state := s.loadLocked()
	return state.Messages[id]
}

// Known reports whether id has a persisted overlay record.
func (s *Store) Known(id string) bool {
	if s == nil || id == "" {
		return false
	}
	mu := s.mutex()
	mu.Lock()
	defer mu.Unlock()
	state := s.loadLocked()
	_, ok := state.Messages[id]
	return ok
}

// Snapshot returns an independent copy of all overlay records.
func (s *Store) Snapshot() map[string]MessageState {
	if s == nil {
		return nil
	}
	mu := s.mutex()
	mu.Lock()
	defer mu.Unlock()
	state := s.loadLocked()
	out := make(map[string]MessageState, len(state.Messages))
	for id, message := range state.Messages {
		out[id] = message
	}
	return out
}

// RememberLocator records the mailbox and UID used to resolve id.
func (s *Store) RememberLocator(id, mailbox, uid string) error {
	if s == nil || id == "" || mailbox == "" || uid == "" {
		return nil
	}
	return s.update(id, func(message MessageState) MessageState {
		message.Mailbox = mailbox
		message.UID = uid
		return message
	}, false)
}

// RememberLocators merges a search batch into the sidecar with one load and at
// most one atomic write. Existing read/archive/trash flags are preserved.
func (s *Store) RememberLocators(locators map[string]MessageState) error {
	if s == nil || len(locators) == 0 {
		return nil
	}
	mu := s.mutex()
	mu.Lock()
	defer mu.Unlock()
	state := s.loadLocked()
	changed := false
	for id, locator := range locators {
		if id == "" || locator.Mailbox == "" || locator.UID == "" {
			continue
		}
		message := state.Messages[id]
		if message.Mailbox == locator.Mailbox && message.UID == locator.UID {
			continue
		}
		message.Mailbox = locator.Mailbox
		message.UID = locator.UID
		state.Messages[id] = message
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveLocked(state)
}

// MarkRead records the local read overlay for id.
func (s *Store) MarkRead(id string) error {
	return s.update(id, func(message MessageState) MessageState {
		message.Read = true
		return message
	}, true)
}

// MarkArchived records the local archive overlay for id.
func (s *Store) MarkArchived(id string) error {
	return s.update(id, func(message MessageState) MessageState {
		message.Read = true
		message.Archived = true
		return message
	}, true)
}

// MarkTrashed records the local trash overlay for id.
func (s *Store) MarkTrashed(id string) error {
	return s.update(id, func(message MessageState) MessageState {
		message.Read = true
		message.Trashed = true
		return message
	}, true)
}

func (s *Store) update(id string, mutate func(MessageState) MessageState, touch bool) error {
	if s == nil || id == "" {
		return nil
	}
	mu := s.mutex()
	mu.Lock()
	defer mu.Unlock()
	state := s.loadLocked()
	message := mutate(state.Messages[id])
	if touch {
		message.UpdatedAtMS = time.Now().UnixMilli()
	}
	state.Messages[id] = message
	return s.saveLocked(state)
}

func (s *Store) loadLocked() diskState {
	state := diskState{Messages: map[string]MessageState{}}
	if s == nil || s.path == "" {
		return state
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	if state.Messages == nil {
		state.Messages = map[string]MessageState{}
	}
	return state
}

func (s *Store) saveLocked(state diskState) error {
	if s == nil || s.path == "" {
		return nil
	}
	if state.Messages == nil {
		state.Messages = map[string]MessageState{}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(s.path, data, &atomicfile.Options{Perm: 0o600, DirPerm: 0o700})
}
