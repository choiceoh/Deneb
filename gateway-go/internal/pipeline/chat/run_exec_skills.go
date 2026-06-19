package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
			WorkspaceDir:     workspaceDir,
			BundledSkillsDir: bundledSkillsDir(),
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
		// skills tool's read action or the read tool against the listed
		// location path.
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

// EligibleWorkspaceSkills discovers workspace skills and applies the same
// archived + eligibility filtering loadCachedSkillsPrompt uses, so read-only
// consumers (the Settings Skills tab via miniapp.skills.list) advertise only
// skills the agent can actually use — not archived or ineligible ones.
//
// availableToolNames must be the agent's registered tools (Handler.ToolNames).
// FilterEligibleSkills only enforces requires_tools / fallback_for_tools when
// the AvailableTools map is non-empty, so passing the real toolset is what keeps
// a requires_tools skill out of the list when its tool isn't registered —
// matching the prompt and slash-command routing. Passing nil would skip that
// check and over-advertise.
func EligibleWorkspaceSkills(workspaceDir string, availableToolNames []string) []skills.SkillEntry {
	availableTools := make(map[string]struct{}, len(availableToolNames))
	for _, name := range availableToolNames {
		availableTools[name] = struct{}{}
	}
	entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
		WorkspaceDir:     workspaceDir,
		BundledSkillsDir: bundledSkillsDir(),
	})
	entries = skills.FilterExcludedSkills(entries, loadArchivedCuratorSkillNames())
	entries = skills.FilterEligibleSkills(entries, skills.EligibilityContext{
		EnvVars:        skills.EnvSnapshotFromOS(),
		SkillConfigs:   make(map[string]skills.SkillConfig),
		AvailableTools: availableTools,
	})
	return entries
}

// bundledSkillsDir returns the repo's checked-in skills/ directory so the agent
// prompt and the Settings → Skills tab surface the bundled skills WITHOUT
// copying them into the runtime workspace (~/.deneb/workspace/skills). Discovery
// merges this SourceBundled set under the workspace (bundled < workspace), so a
// workspace copy still overrides a bundled skill of the same name.
//
// DENEB_BUNDLED_SKILLS_DIR overrides the path; otherwise it probes next to the
// gateway binary (dist/deneb-gateway → ../skills) and the working directory
// (deploy runs the binary from the repo root). Returns "" when no skills/ dir is
// found — discovery then simply skips the bundled source.
func bundledSkillsDir() string {
	if dir := strings.TrimSpace(os.Getenv("DENEB_BUNDLED_SKILLS_DIR")); dir != "" {
		return dir
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "skills"),
			filepath.Join(exeDir, "..", "skills"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "skills"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
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
