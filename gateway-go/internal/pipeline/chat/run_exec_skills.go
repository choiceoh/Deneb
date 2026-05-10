package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// loadCachedSkillsPrompt returns the cached skills prompt, rebuilding it when
// the watcher version changes or on first call.
// availableToolNames is used for conditional activation (requires_tools/fallback_for_tools).
func loadCachedSkillsPrompt(workspaceDir string, availableToolNames []string) string {
	curatorVersion := skillCuratorStateVersion()
	skillsCache.mu.RLock()
	if skillsCache.built && skillsCache.version == curatorVersion {
		prompt := skillsCache.prompt
		skillsCache.mu.RUnlock()
		return prompt
	}
	skillsCache.mu.RUnlock()

	skillsCache.mu.Lock()
	defer skillsCache.mu.Unlock()

	// Double-check after acquiring write lock.
	if skillsCache.built && skillsCache.version == curatorVersion {
		return skillsCache.prompt
	}

	// Build available tools map for conditional activation.
	availableTools := make(map[string]struct{}, len(availableToolNames))
	for _, name := range availableToolNames {
		availableTools[name] = struct{}{}
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
		ExcludedSkills: loadArchivedCuratorSkillNames(),
	}
	// Discover entries first so we can cache them for slash command routing.
	allEntries := skills.DiscoverWorkspaceSkills(cfg.DiscoverConfig)
	allEntries = skills.FilterExcludedSkills(allEntries, cfg.ExcludedSkills)
	SetCachedSkillEntries(allEntries, 0)

	snapshot := skills.BuildWorkspaceSkillSnapshot(cfg)
	if snapshot != nil {
		// P5 — compact-index format for the semi-static prompt block.
		// snapshot.Prompt embeds the full XML (name + category + tags +
		// related_skills + description + location); for in-prompt
		// scanning the agent only uses name + description + location.
		// We rebuild from snapshot.ResolvedSkills with BuildSkillsIndex
		// so the semi-static block is roughly half the size and keeps
		// drifting less when peripheral metadata changes (tags edits,
		// category renames). Full body is loaded on demand via the
		// skills tool's read action or the read tool against <location>.
		indexResult := skills.BuildSkillsIndex(snapshot.ResolvedSkills, skills.DefaultSkillsLimits())
		skillsCache.prompt = indexResult.Prompt
		skillsCache.snapshot = snapshot
	} else {
		skillsCache.prompt = ""
		skillsCache.snapshot = nil
	}
	skillsCache.built = true
	skillsCache.version = curatorVersion
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
	skillsCache.version = 0
	skillsCache.mu.Unlock()
}

// availableToolNames returns sorted tool names from the registry, or nil if nil.
func availableToolNames(tools *ToolRegistry) []string {
	if tools == nil {
		return nil
	}
	return tools.SortedNames()
}

func skillCuratorStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", "skill_curator_state.json")
}

func skillCuratorStateVersion() int64 {
	path := skillCuratorStatePath()
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func loadArchivedCuratorSkillNames() map[string]struct{} {
	path := skillCuratorStatePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var state struct {
		Skills map[string]struct {
			State string `json:"state"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	archived := make(map[string]struct{})
	for name, rec := range state.Skills {
		if rec.State == "archived" {
			archived[name] = struct{}{}
		}
	}
	if len(archived) == 0 {
		return nil
	}
	return archived
}
