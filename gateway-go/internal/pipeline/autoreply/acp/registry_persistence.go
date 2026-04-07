// registry_persistence.go — File-based persistence for ACPRegistry agent state.
//
// On gateway restart, previously spawned subagents are restored to the registry
// so the parent agent retains awareness of its children's existence and lineage.
// Session lifecycle state is still derived from session.Manager on read.
package acp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// DefaultRegistryStorePath returns the default path for the registry store.
func DefaultRegistryStorePath(homeDir string) string {
	return filepath.Join(homeDir, DefaultACPDir, "agents.json")
}

// RegistryStoreFile is the on-disk format for the agent registry.
type RegistryStoreFile struct {
	Version int        `json:"version"`
	Agents  []ACPAgent `json:"agents"`
}

// RegistryStore persists ACPRegistry agent state to disk.
type RegistryStore struct {
	mu         sync.Mutex
	path       string
	cachedHash [sha256.Size]byte
}

// NewRegistryStore creates a new registry store at the given path.
func NewRegistryStore(path string) *RegistryStore {
	return &RegistryStore{path: path}
}

// Load reads agents from disk. Returns nil slice if the file does not exist.
func (s *RegistryStore) Load() ([]ACPAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry store: %w", err)
	}

	var file RegistryStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse registry store: %w", err)
	}

	s.cachedHash = sha256.Sum256(data)
	return file.Agents, nil
}

// Save writes agents to disk using atomic write (temp file + rename).
// Skips write if content hasn't changed since the last Load or Save.
func (s *RegistryStore) Save(agents []ACPAgent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file := RegistryStoreFile{
		Version: 1,
		Agents:  agents,
	}
	if file.Agents == nil {
		file.Agents = []ACPAgent{}
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry store: %w", err)
	}
	data = append(data, '\n')

	// Skip write if unchanged.
	newHash := sha256.Sum256(data)
	if newHash == s.cachedHash {
		return nil
	}

	if err := atomicfile.WriteFile(s.path, data, &atomicfile.Options{Backup: true}); err != nil {
		return fmt.Errorf("save registry store: %w", err)
	}

	s.cachedHash = newHash
	return nil
}

// SyncFromRegistry saves the current state of an ACPRegistry to disk.
// Only saves non-terminal agents (idle or running) since terminal agents
// will be cleaned up by GC anyway.
func (s *RegistryStore) SyncFromRegistry(registry *ACPRegistry) error {
	all := registry.List("")
	// Filter to non-terminal agents worth persisting.
	var active []ACPAgent
	for _, a := range all {
		if a.Status == "idle" || a.Status == "running" || a.Status == "done" {
			active = append(active, a)
		}
	}
	return s.Save(active)
}

// RestoreToRegistry loads agents from disk and registers them in the registry.
// Agents are restored with their original metadata; session lifecycle state
// will be derived from session.Manager when accessed.
func (s *RegistryStore) RestoreToRegistry(registry *ACPRegistry) (int, error) {
	agents, err := s.Load()
	if err != nil {
		return 0, err
	}
	restored := 0
	for _, a := range agents {
		// Set status to "idle" for restored agents — actual status will be
		// derived from session.Manager on the next Get/List call.
		a.Status = "idle"
		registry.Register(a)
		restored++
	}
	return restored, nil
}
