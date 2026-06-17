package mailarchive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// MessageState is the local mutation overlay for the read-only archive store.
// The LMTP/IMAP archive remains immutable; native app actions are reflected by
// this small sidecar file.
type MessageState struct {
	Read        bool   `json:"read,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
	Trashed     bool   `json:"trashed,omitempty"`
	Mailbox     string `json:"mailbox,omitempty"`
	UID         string `json:"uid,omitempty"`
	UpdatedAtMS int64  `json:"updatedAtMs,omitempty"`
}

type stateDisk struct {
	Messages map[string]MessageState `json:"messages"`
}

// StateStore persists archive locators plus local Gmail-like mutations.
type StateStore struct {
	path string
	mu   sync.Mutex
}

func NewStateStore(path string) *StateStore {
	return &StateStore{path: path}
}

func (s *StateStore) Get(id string) MessageState {
	if s == nil || id == "" {
		return MessageState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	return st.Messages[id]
}

func (s *StateStore) Known(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	_, ok := st.Messages[id]
	return ok
}

func (s *StateStore) RememberLocator(id, mailbox, uid string) error {
	if s == nil || id == "" || mailbox == "" || uid == "" {
		return nil
	}
	return s.update(id, func(ms MessageState) MessageState {
		ms.Mailbox = mailbox
		ms.UID = uid
		return ms
	}, false)
}

func (s *StateStore) MarkRead(id string) error {
	return s.update(id, func(ms MessageState) MessageState {
		ms.Read = true
		return ms
	}, true)
}

func (s *StateStore) MarkArchived(id string) error {
	return s.update(id, func(ms MessageState) MessageState {
		ms.Read = true
		ms.Archived = true
		return ms
	}, true)
}

func (s *StateStore) MarkTrashed(id string) error {
	return s.update(id, func(ms MessageState) MessageState {
		ms.Read = true
		ms.Trashed = true
		return ms
	}, true)
}

func (s *StateStore) update(id string, mutate func(MessageState) MessageState, touch bool) error {
	if s == nil || id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	ms := mutate(st.Messages[id])
	if touch {
		ms.UpdatedAtMS = time.Now().UnixMilli()
	}
	st.Messages[id] = ms
	return s.saveLocked(st)
}

func (s *StateStore) loadLocked() stateDisk {
	st := stateDisk{Messages: map[string]MessageState{}}
	if s == nil || s.path == "" {
		return st
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.Messages == nil {
		st.Messages = map[string]MessageState{}
	}
	return st
}

func (s *StateStore) saveLocked(st stateDisk) error {
	if s == nil || s.path == "" {
		return nil
	}
	if st.Messages == nil {
		st.Messages = map[string]MessageState{}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicfile.WriteFile(s.path, b, &atomicfile.Options{Perm: 0o600, DirPerm: 0o700})
}
