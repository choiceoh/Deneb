// acp_persistence.go — File-based persistence for ACP session bindings.
// Follows the same atomic-write pattern as cron/store.go.
package acp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultACPDir is the default directory for ACP data.
const DefaultACPDir = ".deneb/acp"

// DefaultBindingStorePath returns the default path for the binding store.
func DefaultBindingStorePath(homeDir string) string {
	return filepath.Join(homeDir, DefaultACPDir, "bindings.json")
}

// BindingStoreFile is the on-disk format for the binding store.
type BindingStoreFile struct {
	Version  int             `json:"version"`
	Bindings []StoredBinding `json:"bindings"`
}

// BindingStore persists SessionBindingService state to disk.
type BindingStore struct {
	mu         sync.Mutex
	path       string
	cachedHash [sha256.Size]byte
}

// NewBindingStore creates a new binding store at the given path.
func NewBindingStore(path string) *BindingStore {
	return &BindingStore{path: path}
}

// Load reads bindings from disk. Returns nil slice if the file does not exist.
func (s *BindingStore) Load() ([]StoredBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read binding store: %w", err)
	}

	var file BindingStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse binding store: %w", err)
	}

	s.cachedHash = sha256.Sum256(data)
	return file.Bindings, nil
}

// Save writes bindings to disk using atomic write (temp file + rename).
// Skips write if content hasn't changed since the last Load or Save.
func (s *BindingStore) Save(bindings []StoredBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file := BindingStoreFile{
		Version:  1,
		Bindings: bindings,
	}
	if file.Bindings == nil {
		file.Bindings = []StoredBinding{}
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal binding store: %w", err)
	}
	data = append(data, '\n')

	// Skip write if unchanged.
	newHash := sha256.Sum256(data)
	if newHash == s.cachedHash {
		return nil
	}

	// Ensure directory exists.
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create binding store dir: %w", err)
	}

	// Create .bak backup if file exists.
	if _, err := os.Stat(s.path); err == nil {
		_ = copyFile(s.path, s.path+".bak")
	}

	// Atomic write: temp file + rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write binding store temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename binding store: %w", err)
	}

	s.cachedHash = newHash
	return nil
}

// SyncFromService saves the current state of a SessionBindingService to disk.
func (s *BindingStore) SyncFromService(svc *SessionBindingService) error {
	return s.Save(svc.Snapshot())
}

// RestoreToService loads bindings from disk and restores them into the service.
func (s *BindingStore) RestoreToService(svc *SessionBindingService) error {
	bindings, err := s.Load()
	if err != nil {
		return err
	}
	if len(bindings) > 0 {
		svc.RestoreAll(bindings)
	}
	return nil
}

// copyFile copies src to dst. Best-effort; errors are ignored by callers.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
