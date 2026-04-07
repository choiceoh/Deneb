package chat

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// skillsPromptCache is a version-aware cache for the workspace skills prompt.
// Invalidated when the skills watcher bumps the version (file changes detected).
var skillsCache struct {
	mu       sync.RWMutex
	prompt   string
	snapshot *skills.FullSkillSnapshot
	version  int64
	built    bool
}

// skillsWatcher is the shared watcher that monitors SKILL.md file changes.
// Initialized once by InitSkillsWatcher.
var skillsWatcher *skills.Watcher

// InitSkillsWatcher creates and starts the skills watcher for a workspace.
// Call once at server startup. The watcher invalidates the skills prompt cache
// when SKILL.md files change on disk.
func InitSkillsWatcher(workspaceDir string) {
	if skillsWatcher != nil {
		return
	}
	skillsWatcher = skills.NewWatcher(nil)
	skillsWatcher.RegisterChangeListener(func(event skills.SkillsChangeEvent) {
		skillsCache.mu.Lock()
		skillsCache.built = false
		skillsCache.mu.Unlock()
	})
	skillsWatcher.EnsureWatcher(workspaceDir, nil, 250)
}

// loadCachedSkillsPrompt returns the cached skills prompt, rebuilding it when
// the watcher version changes or on first call.
// availableToolNames is used for conditional activation (requires_tools/fallback_for_tools).
func loadCachedSkillsPrompt(workspaceDir string, availableToolNames []string) string {
	skillsCache.mu.RLock()
	if skillsCache.built {
		prompt := skillsCache.prompt
		skillsCache.mu.RUnlock()
		return prompt
	}
	skillsCache.mu.RUnlock()

	skillsCache.mu.Lock()
	defer skillsCache.mu.Unlock()

	// Double-check after acquiring write lock.
	if skillsCache.built {
		return skillsCache.prompt
	}

	// Build available tools map for conditional activation.
	availableTools := make(map[string]bool, len(availableToolNames))
	for _, name := range availableToolNames {
		availableTools[name] = true
	}

	cfg := skills.SnapshotConfig{
		DiscoverConfig: skills.DiscoverConfig{
			WorkspaceDir: workspaceDir,
		},
		Eligibility: skills.EligibilityContext{
			EnvVars:        skills.EnvSnapshotFromOS(),
			SkillConfigs:   make(map[string]skills.SkillConfig),
			AvailableTools: availableTools,
		},
	}
	// Discover entries first so we can cache them for slash command routing.
	allEntries := skills.DiscoverWorkspaceSkills(cfg.DiscoverConfig)
	SetCachedSkillEntries(allEntries, 0)

	snapshot := skills.BuildWorkspaceSkillSnapshot(cfg)
	if snapshot != nil {
		skillsCache.prompt = snapshot.Prompt
		skillsCache.snapshot = snapshot
	} else {
		skillsCache.prompt = ""
		skillsCache.snapshot = nil
	}
	skillsCache.built = true
	return skillsCache.prompt
}

// CachedSkillsSnapshot returns the last-built skills snapshot, or nil.
func CachedSkillsSnapshot() *skills.FullSkillSnapshot {
	skillsCache.mu.RLock()
	defer skillsCache.mu.RUnlock()
	return skillsCache.snapshot
}

// InvalidateSkillsCache forces the skills prompt to be rebuilt on next access.
func InvalidateSkillsCache() {
	skillsCache.mu.Lock()
	skillsCache.built = false
	skillsCache.mu.Unlock()
}

// availableToolNames returns sorted tool names from the registry, or nil if nil.
func availableToolNames(tools *ToolRegistry) []string {
	if tools == nil {
		return nil
	}
	return tools.SortedNames()
}
