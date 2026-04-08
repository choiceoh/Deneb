// sync.go copies skills from a source workspace to a target sandbox workspace.
//
// This ports syncSkillsToWorkspace() from src/agents/skills/workspace.ts.
package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var syncMu sync.Mutex

// SyncSkillsToWorkspace copies discovered skills from source to target workspace.
// The target's skills/ directory is replaced entirely.
func SyncSkillsToWorkspace(sourceDir, targetDir string, cfg DiscoverConfig) error {
	sourceDir = strings.TrimSpace(sourceDir)
	targetDir = strings.TrimSpace(targetDir)
	if sourceDir == "" || targetDir == "" || sourceDir == targetDir {
		return nil
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	log := cfg.logger()
	targetSkillsDir := filepath.Join(targetDir, "skills")

	// Discover skills from source.
	cfg.WorkspaceDir = sourceDir
	entries := DiscoverWorkspaceSkills(cfg)

	// Remove existing target skills directory.
	if err := os.RemoveAll(targetSkillsDir); err != nil {
		return fmt.Errorf("failed to remove target skills dir: %w", err)
	}
	if err := os.MkdirAll(targetSkillsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create target skills dir: %w", err)
	}

	usedNames := make(map[string]struct{})
	for _, entry := range entries {
		dirName := filepath.Base(entry.Skill.Dir)
		if dirName == "" || dirName == "." || dirName == ".." {
			continue
		}
		uniqueName := resolveUniqueDirName(dirName, usedNames)
		usedNames[uniqueName] = struct{}{}

		dest := filepath.Join(targetSkillsDir, uniqueName)
		if err := copyDir(entry.Skill.Dir, dest); err != nil {
			log.Warn("failed to copy skill to sandbox",
				"skill", entry.Skill.Name, "error", err)
			continue
		}
	}

	return nil
}

func resolveUniqueDirName(base string, used map[string]struct{}) string {
	if _, ok := used[base]; !ok {
		return base
	}
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
	return fmt.Sprintf("%s-fallback", base)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := os.ReadFile(path) //nolint:gosec // G122 — path comes from WalkDir, safe in this context
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode()) //nolint:gosec // G703 — destPath is constructed from trusted src/dst paths
	})
}

// SkillStatus represents the aggregated status of all skills for a workspace.
type SkillStatus struct {
	Skills        []SkillStatusEntry `json:"skills"`
	RequiredBins  []string           `json:"requiredBins,omitempty"`
	TotalCount    int                `json:"totalCount"`
	EligibleCount int                `json:"eligibleCount"`
}

// SkillStatusEntry is one skill's status report.
type SkillStatusEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Eligible    bool   `json:"eligible"`
	Emoji       string `json:"emoji,omitempty"`
	Description string `json:"description,omitempty"`
	PrimaryEnv  string `json:"primaryEnv,omitempty"`
}

// BuildWorkspaceSkillStatus builds a status report for all discovered skills.
func BuildWorkspaceSkillStatus(cfg DiscoverConfig, eligCtx EligibilityContext) *SkillStatus {
	allEntries := DiscoverWorkspaceSkills(cfg)

	binsSet := make(map[string]struct{})
	var statusEntries []SkillStatusEntry
	eligibleCount := 0

	for _, entry := range allEntries {
		eligible := ShouldIncludeSkill(entry, eligCtx)
		if eligible {
			eligibleCount++
		}

		se := SkillStatusEntry{
			Name:        entry.Skill.Name,
			Source:      string(entry.Skill.Source),
			Eligible:    eligible,
			Description: entry.Skill.Description,
		}
		if entry.Metadata != nil {
			se.Emoji = entry.Metadata.Emoji
			se.PrimaryEnv = entry.Metadata.PrimaryEnv
			if entry.Metadata.Requires != nil {
				for _, bin := range entry.Metadata.Requires.Bins {
					binsSet[bin] = struct{}{}
				}
				for _, bin := range entry.Metadata.Requires.AnyBins {
					binsSet[bin] = struct{}{}
				}
			}
		}
		statusEntries = append(statusEntries, se)
	}

	var requiredBins []string
	for bin := range binsSet {
		requiredBins = append(requiredBins, bin)
	}

	return &SkillStatus{
		Skills:        statusEntries,
		RequiredBins:  requiredBins,
		TotalCount:    len(allEntries),
		EligibleCount: eligibleCount,
	}
}
