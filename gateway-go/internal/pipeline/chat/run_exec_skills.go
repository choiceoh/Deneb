package chat

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// skillsPromptCache is a version-aware cache for the workspace skills prompt.
// Invalidated when the skills watcher bumps the version (file changes detected).
var skillsCache struct {
	mu       sync.RWMutex
	prompt   string
	snapshot *skills.FullSkillSnapshot
	version  int64
	// toolsKey fingerprints the registered tool set the cached catalog was
	// built from. requires_tools / fallback_for_tools eligibility is computed
	// against that set, so it is a build INPUT — but the cache used to key on
	// the curator version alone and the tool set provably varies at runtime:
	// external MCP tools register from a background goroutine after
	// ListTools returns (runtime/externalmcp), so a first turn that wins the
	// race bakes an MCP-less catalog in until a skills file change bumps the
	// curator version. A skill gated on an MCP tool then stays invisible long
	// after its tool exists.
	toolsKey uint64
	built    bool
}

// skillToolSetKey fingerprints the registered tool names. Order-independent so
// a registry that returns the same set in a different order does not force a
// rebuild, and length-mixed so add/remove of a name always moves the key.
func skillToolSetKey(names []string) uint64 {
	var sum uint64
	for _, name := range names {
		h := fnv.New64a()
		_, _ = h.Write([]byte(name))
		sum += h.Sum64()
	}
	return sum ^ uint64(len(names))
}

// loadCachedSkillsPrompt returns the cached skills prompt, rebuilding it when
// the watcher version changes or on first call.
// availableToolNames is used for conditional activation (requires_tools/fallback_for_tools).
func loadCachedSkillsPrompt(workspaceDir string, availableToolNames []string) string {
	curatorVersion := skillCuratorStateVersion()
	toolsKey := skillToolSetKey(availableToolNames)
	skillsCache.mu.RLock()
	if skillsCache.built && skillsCache.version == curatorVersion && skillsCache.toolsKey == toolsKey {
		prompt := skillsCache.prompt
		skillsCache.mu.RUnlock()
		return prompt
	}
	skillsCache.mu.RUnlock()

	skillsCache.mu.Lock()
	defer skillsCache.mu.Unlock()

	// Double-check after acquiring write lock.
	if skillsCache.built && skillsCache.version == curatorVersion && skillsCache.toolsKey == toolsKey {
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
			BundledSkillsDir: BundledSkillsDir(),
		},
		Eligibility: skills.EligibilityContext{
			EnvVars:        skills.EnvSnapshotFromOS(),
			SkillConfigs:   make(map[string]skills.SkillConfig),
			AvailableTools: availableTools,
		},
		ExcludedSkills: excludedSkillNames(),
	}
	// Discover entries first so we can cache them for slash command routing.
	discovered := skills.DiscoverWorkspaceSkills(cfg.DiscoverConfig)
	allEntries := skills.FilterExcludedSkills(discovered, cfg.ExcludedSkills)
	logSuppressedSkills(discovered, allEntries)
	SetCachedSkillEntries(allEntries, 0)

	snapshot := skills.BuildWorkspaceSkillSnapshot(cfg)
	if snapshot != nil {
		// Keep only names in the ambient semi-static manifest. Exact triggers
		// load at most two bounded bodies in the per-turn tail; unmatched complex
		// work searches the skills tool on demand. Oversized bodies and auxiliary
		// files retain the explicit read path.
		indexResult := skills.BuildSkillsIndex(snapshot.ResolvedSkills, skills.DefaultSkillsLimits())
		skillsCache.prompt = indexResult.Prompt
		skillsCache.snapshot = snapshot
	} else {
		skillsCache.prompt = ""
		skillsCache.snapshot = nil
	}
	skillsCache.built = true
	skillsCache.version = curatorVersion
	skillsCache.toolsKey = toolsKey
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
		BundledSkillsDir: BundledSkillsDir(),
	})
	entries = skills.FilterExcludedSkills(entries, excludedSkillNames())
	entries = skills.FilterEligibleSkills(entries, skills.EligibilityContext{
		EnvVars:        skills.EnvSnapshotFromOS(),
		SkillConfigs:   make(map[string]skills.SkillConfig),
		AvailableTools: availableTools,
	})
	return entries
}

// BundledSkillsDir returns the repo's checked-in skills/ directory so the agent
// prompt and the Settings → Skills tab surface the bundled skills WITHOUT
// copying them into the runtime workspace (~/.deneb/workspace/skills). Discovery
// merges this SourceBundled set under the workspace (bundled < workspace), so a
// workspace copy still overrides a bundled skill of the same name.
//
// DENEB_BUNDLED_SKILLS_DIR overrides the path; otherwise it probes next to the
// gateway binary (dist/deneb-gateway → ../skills) and the working directory
// (deploy runs the binary from the repo root). Returns "" when no skills/ dir is
// found — discovery then simply skips the bundled source.
func BundledSkillsDir() string {
	if dir := strings.TrimSpace(os.Getenv("DENEB_BUNDLED_SKILLS_DIR")); dir != "" {
		return dir
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(
			candidates,
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
	skillsCache.toolsKey = 0
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

// logSuppressedSkills names the skills that were discovered on disk but kept
// out of every surface, split by WHY.
//
// Suppression is silent by design (a deleted skill "disappears from every
// surface at once"), and that silence has a cost: on 2026-08-18 the operator
// typed "지식 인터뷰 … 인터뷰로 정리하자" — two exact triggers of the
// kb-interview skill, still shipped in the repo — and nothing fired, because
// the skill had been tombstoned on 07-21. Nothing anywhere said so. One line
// per catalog rebuild (boot / curator-state change, not per turn) makes the
// divergence between "shipped in skills/" and "live in the catalog" visible
// where every other startup fact already is.
func logSuppressedSkills(discovered, kept []skills.SkillEntry) {
	if len(discovered) == len(kept) {
		return
	}
	live := make(map[string]struct{}, len(kept))
	for _, e := range kept {
		live[e.Skill.Name] = struct{}{}
	}
	// Reason and age come along: "왜 꺼졌는지" is the half that was missing when
	// six skills sat suppressed for five weeks (deleted.go).
	deleted := make(map[string]skills.DeletedSkill)
	for _, d := range skills.LoadDeletedSkills() {
		deleted[d.Name] = d
	}
	var tombstoned, archived []string
	for _, e := range discovered {
		name := e.Skill.Name
		if _, ok := live[name]; ok {
			continue
		}
		if d, ok := deleted[name]; ok {
			tombstoned = append(tombstoned, describeTombstone(d, time.Now()))
			continue
		}
		archived = append(archived, name)
	}
	sort.Strings(tombstoned)
	sort.Strings(archived)
	slog.Info("skills suppressed from every surface",
		"tombstoned", strings.Join(tombstoned, ","),
		"curatorArchived", strings.Join(archived, ","),
		"discovered", len(discovered), "live", len(kept))
}

// excludedSkillNames is the union of curator-archived skills and
// operator-deleted skills (skills.LoadDeletedSkillNames — the bundled-skill
// tombstone file), applied identically to the prompt, the skills tab, and
// slash routing so a deleted skill disappears from every surface at once.
func excludedSkillNames() map[string]struct{} {
	excluded := loadArchivedCuratorSkillNames()
	for name := range skills.LoadDeletedSkillNames() {
		if excluded == nil {
			excluded = make(map[string]struct{})
		}
		excluded[name] = struct{}{}
	}
	return excluded
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

// describeTombstone renders one suppression as "name(사유, N일째)". An entry
// written before tombstones carried metadata has neither — it still names
// itself, which is the point.
func describeTombstone(d skills.DeletedSkill, now time.Time) string {
	parts := make([]string, 0, 2)
	if d.Reason != "" {
		parts = append(parts, d.Reason)
	}
	if at, err := time.Parse(time.RFC3339, d.At); err == nil {
		if days := int(now.Sub(at).Hours() / 24); days >= 0 {
			parts = append(parts, fmt.Sprintf("%d일째", days))
		}
	}
	if len(parts) == 0 {
		return d.Name + "(기록 없음)"
	}
	return d.Name + "(" + strings.Join(parts, ", ") + ")"
}
