package lmtpd

import (
	"encoding/json"
	"os"
	"sync"
)

// SeenStore is a small persisted, bounded set of recently-seen dedup keys
// (sanitized Message-IDs). It lets the LMTP handler skip a message that an MTA
// re-delivers — across restarts — so the same mail isn't analyzed (or wiki-paged,
// or chat-reported) twice. Best-effort: a missing/corrupt file just starts empty,
// and a failed save is non-fatal (at worst a duplicate re-delivery slips through).
type SeenStore struct {
	mu   sync.Mutex
	path string
	max  int
	set  map[string]struct{}
	ring []string
}

// NewSeenStore loads (or starts) a dedup set persisted at path, keeping the most
// recent max keys.
func NewSeenStore(path string, max int) *SeenStore {
	if max <= 0 {
		max = 2000
	}
	s := &SeenStore{path: path, max: max, set: map[string]struct{}{}}
	if b, err := os.ReadFile(path); err == nil {
		var keys []string
		if json.Unmarshal(b, &keys) == nil {
			for _, k := range keys {
				if _, ok := s.set[k]; !ok {
					s.set[k] = struct{}{}
					s.ring = append(s.ring, k)
				}
			}
		}
	}
	return s
}

// Seen reports whether key was already recorded.
func (s *SeenStore) Seen(key string) bool {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[key]
	return ok
}

// Mark records key (evicting the oldest past max) and persists the set.
func (s *SeenStore) Mark(key string) { s.MarkIfNew(key) }

// MarkIfNew atomically records key and reports whether it was new (not seen
// before). The combined check-and-set under one lock is the dedup primitive:
// concurrent deliveries of the same key won't both pass. An empty key is unkeyed
// (no Message-ID) — treated as always-new since it can't be deduped.
func (s *SeenStore) MarkIfNew(key string) bool {
	if key == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[key]; ok {
		return false
	}
	s.set[key] = struct{}{}
	s.ring = append(s.ring, key)
	for len(s.ring) > s.max {
		oldest := s.ring[0]
		s.ring = s.ring[1:]
		delete(s.set, oldest)
	}
	s.persistLocked()
	return true
}

func (s *SeenStore) persistLocked() {
	b, err := json.Marshal(s.ring)
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, s.path) // atomic swap; best-effort
}
