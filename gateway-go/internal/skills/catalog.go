// Package skills provides skill discovery, metadata parsing, filtering,
// and filesystem watching for the Go gateway.
//
// This mirrors the skill system in src/agents/skills/ from the TypeScript codebase.
// Skills are user-defined or bundled plugins that extend agent capabilities.
package skills

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Skill represents a loaded skill entry from disk.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Dir         string `json:"dir"`
	FilePath    string `json:"filePath,omitempty"`
	// Source indicates where the skill was discovered.
	Source SkillSource `json:"source"`
}

// SkillSource indicates the origin of a skill.
type SkillSource string

const (
	SourceBundled   SkillSource = "bundled"
	SourceManaged   SkillSource = "managed"
	SourceWorkspace SkillSource = "workspace"
	SourcePlugin    SkillSource = "plugin"
	SourceExtra     SkillSource = "extra"
	SourcePersonal  SkillSource = "agents-skills-personal"
	SourceProject   SkillSource = "agents-skills-project"
)

// SkillEntry is a fully resolved skill with parsed metadata.
type SkillEntry struct {
	Skill      Skill                  `json:"skill"`
	Frontmatter ParsedFrontmatter    `json:"frontmatter,omitempty"`
	Metadata   *DenebSkillMetadata    `json:"metadata,omitempty"`
	Invocation *SkillInvocationPolicy `json:"invocation,omitempty"`
}

// SkillSnapshot represents a point-in-time view of the skill catalog.
type SkillSnapshot struct {
	Entries []SkillEntry `json:"entries"`
	Version int64        `json:"version"`
}

// Catalog manages the skill catalog and provides filtered views.
type Catalog struct {
	mu       sync.RWMutex
	entries  map[string]*SkillEntry // keyed by skill key
	version  int64
	logger   *slog.Logger
}

// NewCatalog creates a new skill catalog.
func NewCatalog(logger *slog.Logger) *Catalog {
	if logger == nil {
		logger = slog.Default()
	}
	return &Catalog{
		entries: make(map[string]*SkillEntry),
		logger:  logger,
	}
}

// Register adds or replaces a skill entry in the catalog.
func (c *Catalog) Register(entry SkillEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := ResolveSkillKey(entry)
	c.entries[key] = &entry
	c.logger.Debug("skill registered", "key", key, "source", entry.Skill.Source)
}

// Unregister removes a skill by key.
func (c *Catalog) Unregister(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; ok {
		delete(c.entries, key)
		return true
	}
	return false
}

// List returns all registered skill entries sorted by key.
func (c *Catalog) List() []SkillEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]SkillEntry, 0, len(c.entries))
	for _, e := range c.entries {
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool {
		return ResolveSkillKey(result[i]) < ResolveSkillKey(result[j])
	})
	return result
}

// Get returns a skill entry by key.
func (c *Catalog) Get(key string) (*SkillEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	copy := *e
	return &copy, true
}

// Snapshot returns a point-in-time snapshot of the catalog.
func (c *Catalog) Snapshot() SkillSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entries := make([]SkillEntry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return ResolveSkillKey(entries[i]) < ResolveSkillKey(entries[j])
	})
	return SkillSnapshot{
		Entries: entries,
		Version: c.version,
	}
}

// SetVersion sets the catalog version.
func (c *Catalog) SetVersion(v int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version = v
}

// Version returns the current catalog version.
func (c *Catalog) Version() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// BuildWorkspaceSnapshot builds a filtered skill catalog for a workspace/agent,
// applying the given skill filter (nil = unrestricted, empty = no skills).
func (c *Catalog) BuildWorkspaceSnapshot(filter []string) SkillSnapshot {
	all := c.List()
	if filter == nil {
		return SkillSnapshot{Entries: all, Version: c.Version()}
	}
	normalized := NormalizeSkillFilter(filter)
	if len(normalized) == 0 {
		return SkillSnapshot{Entries: nil, Version: c.Version()}
	}

	filterSet := make(map[string]bool, len(normalized))
	for _, f := range normalized {
		filterSet[strings.ToLower(f)] = true
	}

	var filtered []SkillEntry
	for _, e := range all {
		key := strings.ToLower(ResolveSkillKey(e))
		if filterSet[key] {
			filtered = append(filtered, e)
		}
	}
	return SkillSnapshot{Entries: filtered, Version: c.Version()}
}

// ResolveSkillKey returns the unique identifier for a skill entry.
// Uses metadata.SkillKey if present, otherwise the skill name.
func ResolveSkillKey(entry SkillEntry) string {
	if entry.Metadata != nil && entry.Metadata.SkillKey != "" {
		return entry.Metadata.SkillKey
	}
	return entry.Skill.Name
}

// Count returns the number of registered skills.
func (c *Catalog) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
