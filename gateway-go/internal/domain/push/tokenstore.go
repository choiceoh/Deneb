package push

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// DeviceToken is one registered native-client FCM registration ID.
type DeviceToken struct {
	Token          string `json:"token"`
	Platform       string `json:"platform,omitempty"` // "android" / "ios" / "desktop"
	RegisteredAtMs int64  `json:"registeredAtMs"`
	LastSeenMs     int64  `json:"lastSeenMs"`
}

// Store is a durable, single-writer set of registered device tokens, persisted
// as a JSON snapshot (atomic .tmp+rename, 0600 — the tokens are opaque
// registration IDs and treated as sensitive). Single-user, single-machine: a
// process-wide RWMutex is sufficient.
//
// The file is loaded lazily on first use; a missing or corrupt file yields an
// empty store so registration keeps working regardless (the corruption is
// overwritten by the next successful write).
type Store struct {
	mu     sync.Mutex
	path   string
	loaded bool
	tokens map[string]DeviceToken // keyed by token string
	now    func() int64           // injectable clock (ms) for tests
}

// NewStore returns a store backed by path. It never fails: load problems are
// deferred to first use and degrade to an empty set.
func NewStore(path string) *Store {
	return &Store{
		path:   path,
		tokens: map[string]DeviceToken{},
		now:    func() int64 { return time.Now().UnixMilli() },
	}
}

func (s *Store) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return // missing file → empty store
	}
	var list []DeviceToken
	if json.Unmarshal(raw, &list) != nil {
		return // corrupt → start empty; next persist overwrites cleanly
	}
	for _, t := range list {
		if strings.TrimSpace(t.Token) != "" {
			s.tokens[t.Token] = t
		}
	}
}

// Register adds or refreshes a device token and persists. Returns the total
// token count after the write.
func (s *Store) Register(token, platform string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, fmt.Errorf("push: empty token")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()

	now := s.now()
	entry, ok := s.tokens[token]
	if !ok {
		entry = DeviceToken{Token: token, RegisteredAtMs: now}
	}
	entry.LastSeenMs = now
	if platform = strings.TrimSpace(platform); platform != "" {
		entry.Platform = platform
	}
	s.tokens[token] = entry
	if err := s.persistLocked(); err != nil {
		return len(s.tokens), err
	}
	return len(s.tokens), nil
}

// Unregister removes a device token and persists when it existed. Returns the
// count after.
func (s *Store) Unregister(token string) (int, error) {
	token = strings.TrimSpace(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()

	if _, ok := s.tokens[token]; ok {
		delete(s.tokens, token)
		if err := s.persistLocked(); err != nil {
			return len(s.tokens), err
		}
	}
	return len(s.tokens), nil
}

// Tokens returns a snapshot of the registered tokens (ordered by registration
// time for deterministic iteration).
func (s *Store) Tokens() []DeviceToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	return s.snapshotLocked()
}

// Prune removes the given tokens (reported dead by FCM) and persists if any
// changed. Returns how many were removed.
func (s *Store) Prune(tokens []string) (int, error) {
	if len(tokens) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()

	removed := 0
	for _, t := range tokens {
		if _, ok := s.tokens[strings.TrimSpace(t)]; ok {
			delete(s.tokens, strings.TrimSpace(t))
			removed++
		}
	}
	if removed > 0 {
		if err := s.persistLocked(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (s *Store) snapshotLocked() []DeviceToken {
	out := make([]DeviceToken, 0, len(s.tokens))
	for _, t := range s.tokens {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RegisteredAtMs != out[j].RegisteredAtMs {
			return out[i].RegisteredAtMs < out[j].RegisteredAtMs
		}
		return out[i].Token < out[j].Token
	})
	return out
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.snapshotLocked(), "", "  ")
	if err != nil {
		return fmt.Errorf("push: marshal tokens: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("push: write tokens: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("push: rename tokens: %w", err)
	}
	return nil
}
