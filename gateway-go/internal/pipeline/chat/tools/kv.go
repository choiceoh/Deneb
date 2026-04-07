package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// kvStore is the in-memory key-value store backed by a JSON file.
// Thread-safe for concurrent tool calls.
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

func (s *kvStore) delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return false
	}
	delete(s.data, key)
	if err := s.save(); err != nil {
		slog.Warn("kv: failed to persist after delete", "key", key, "err", err)
	}
	return true
}

func (s *kvStore) list(prefix string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range s.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			result[k] = v
		}
	}
	return result
}

// kvToolSchema returns the JSON Schema for the kv tool.

// ToolKV implements the kv tool for lightweight key-value persistence.
func ToolKV() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Value  string `json:"value"`
			Prefix string `json:"prefix"`
		}
		if err := jsonutil.UnmarshalInto("kv params", input, &p); err != nil {
			return "", err
		}

		store := getKVStore()

		switch p.Action {
		case "get":
			if p.Key == "" {
				return "", fmt.Errorf("key is required for get")
			}
			v, ok := store.get(p.Key)
			if !ok {
				return fmt.Sprintf("Key %q not found.", p.Key), nil
			}
			return v, nil

		case "set":
			if p.Key == "" {
				return "", fmt.Errorf("key is required for set")
			}
			if err := store.set(p.Key, p.Value); err != nil {
				return "", fmt.Errorf("failed to save: %w", err)
			}
			return fmt.Sprintf("Stored %q.", p.Key), nil

		case "delete":
			if p.Key == "" {
				return "", fmt.Errorf("key is required for delete")
			}
			if !store.delete(p.Key) {
				return fmt.Sprintf("Key %q not found.", p.Key), nil
			}
			return fmt.Sprintf("Deleted %q.", p.Key), nil

		case "list":
			entries := store.list(p.Prefix)
			if len(entries) == 0 {
				if p.Prefix != "" {
					return fmt.Sprintf("No keys matching prefix %q.", p.Prefix), nil
				}
				return "No keys stored.", nil
			}
			// Sort keys for stable output.
			keys := make([]string, 0, len(entries))
			for k := range entries {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var sb strings.Builder
			for _, k := range keys {
				fmt.Fprintf(&sb, "%s = %s\n", k, entries[k])
			}
			return sb.String(), nil

		default:
			return fmt.Sprintf("Unknown kv action: %q. Supported: get, set, delete, list.", p.Action), nil
		}
	}
}
