// Package discord — persistent thread→parent channel mapping.
//
// Stores the mapping from Discord thread IDs to their parent channel IDs
// in a JSON file so that workspace resolution survives server restarts.
// Entries older than 7 days are automatically pruned on load.
package discord

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// threadStoreMaxAge is the maximum age of a thread entry before it's pruned.
	threadStoreMaxAge = 7 * 24 * time.Hour
)

// ThreadEntry stores the parent channel mapping for a Discord thread.
type ThreadEntry struct {
	ParentID  string    `json:"parentId"`
	CreatedAt time.Time `json:"createdAt"`
}

// ThreadStore persists thread→parent channel mappings to disk.
// Thread-safe for concurrent access.
type ThreadStore struct {
	mu       sync.Mutex
	entries  map[string]ThreadEntry // threadID → entry
	filePath string
	logger   *slog.Logger
}

// NewThreadStore creates a store backed by the given file path.
// Loads existing entries from disk and prunes stale ones.
// If filePath is empty, defaults to ~/.deneb/discord-threads.json.
func NewThreadStore(filePath string, logger *slog.Logger) *ThreadStore {
	if filePath == "" {
		home, _ := os.UserHomeDir()
		filePath = filepath.Join(home, ".deneb", "discord-threads.json")
	}
	ts := &ThreadStore{
		entries:  make(map[string]ThreadEntry),
		filePath: filePath,
		logger:   logger,
	}
	ts.load()
	return ts
}

// Put adds or updates a thread→parent mapping and persists to disk.
func (ts *ThreadStore) Put(threadID, parentID string) {
	ts.mu.Lock()
	ts.entries[threadID] = ThreadEntry{
		ParentID:  parentID,
		CreatedAt: time.Now(),
	}
	ts.mu.Unlock()
	ts.save()
}

// Get returns the parent channel ID for a thread, or "" if unknown.
func (ts *ThreadStore) Get(threadID string) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if entry, ok := ts.entries[threadID]; ok {
		return entry.ParentID
	}
	return ""
}

// Delete removes a thread entry and persists to disk.
func (ts *ThreadStore) Delete(threadID string) {
	ts.mu.Lock()
	delete(ts.entries, threadID)
	ts.mu.Unlock()
	ts.save()
}

// Count returns the number of stored entries.
func (ts *ThreadStore) Count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.entries)
}

// load reads entries from disk and prunes stale ones.
func (ts *ThreadStore) load() {
	data, err := os.ReadFile(ts.filePath)
	if err != nil {
		return // file doesn't exist yet
	}
	var entries map[string]ThreadEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		ts.logger.Warn("discord: failed to parse thread store", "path", ts.filePath, "error", err)
		return
	}

	// Prune stale entries.
	now := time.Now()
	pruned := 0
	for id, entry := range entries {
		if now.Sub(entry.CreatedAt) > threadStoreMaxAge {
			delete(entries, id)
			pruned++
		}
	}
	ts.mu.Lock()
	ts.entries = entries
	ts.mu.Unlock()

	if pruned > 0 {
		ts.logger.Info("discord: pruned stale thread entries", "pruned", pruned, "remaining", len(entries))
		ts.save()
	}
	if len(entries) > 0 {
		ts.logger.Info("discord: loaded thread store", "count", len(entries))
	}
}

// save persists entries to disk atomically (write temp + rename).
func (ts *ThreadStore) save() {
	ts.mu.Lock()
	data, err := json.MarshalIndent(ts.entries, "", "  ")
	ts.mu.Unlock()
	if err != nil {
		ts.logger.Warn("discord: failed to marshal thread store", "error", err)
		return
	}

	// Ensure directory exists.
	dir := filepath.Dir(ts.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ts.logger.Warn("discord: failed to create thread store dir", "dir", dir, "error", err)
		return
	}

	// Atomic write: temp file + rename.
	tmpPath := ts.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		ts.logger.Warn("discord: failed to write thread store", "path", tmpPath, "error", err)
		return
	}
	if err := os.Rename(tmpPath, ts.filePath); err != nil {
		ts.logger.Warn("discord: failed to rename thread store", "error", err)
		os.Remove(tmpPath)
	}
}
