package tools

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// kvStore is an in-memory key-value store backed by a JSON file used by
// internal helpers (e.g. gmail contact alias cache). The user-facing kv agent
// tool has been removed; the store is kept as a package-internal singleton.
type kvStore struct {
	mu   sync.RWMutex
	data map[string]string
	path string
}

var (
	globalKV     *kvStore
	globalKVOnce sync.Once
)

// getKVStore returns the singleton KV store, initializing it lazily.
func getKVStore() *kvStore {
	globalKVOnce.Do(func() {
		home, _ := os.UserHomeDir()
		kvPath := filepath.Join(home, ".deneb", "kv.json")
		globalKV = &kvStore{
			data: make(map[string]string),
			path: kvPath,
		}
		globalKV.load()
	})
	return globalKV
}

// load reads the KV file from disk into memory.
func (s *kvStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // file doesn't exist yet
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		slog.Warn("kv: failed to parse store", "path", s.path, "err", err)
	}
}

// save persists the in-memory store to disk.
func (s *kvStore) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644) //nolint:gosec // G306 — world-readable is intentional
}

func (s *kvStore) get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *kvStore) set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.save()
}
