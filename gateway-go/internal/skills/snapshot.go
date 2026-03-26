// snapshot.go orchestrates discovery + eligibility + prompt building
// to produce a complete SkillSnapshot for a workspace.
//
// This ports buildWorkspaceSkillSnapshot() from src/agents/skills/workspace.ts.
package skills

import (
	"strings"
)

// SkillSummary is a lightweight representation of a skill in a snapshot.
type SkillSummary struct {
	Name        string   `json:"name"`
	PrimaryEnv  string   `json:"primaryEnv,omitempty"`
	RequiredEnv []string `json:"requiredEnv,omitempty"`
}

// FullSkillSnapshot is the complete result of building a workspace skill snapshot,
// matching the TypeScript SkillSnapshot type.
type FullSkillSnapshot struct {
	Prompt         string         `json:"prompt"`
	Skills         []SkillSummary `json:"skills"`
	SkillFilter    []string       `json:"skillFilter,omitempty"`
	ResolvedSkills []PromptSkill  `json:"resolvedSkills,omitempty"`
	Version        int64          `json:"version,omitempty"`
}

// SnapshotConfig holds all the configuration for building a snapshot.
type SnapshotConfig struct {
	DiscoverConfig
	SkillFilter     []string // nil = unrestricted, empty = no skills
	Eligibility     EligibilityContext
	SnapshotVersion int64
	RemoteNote      string // optional note from remote eligibility
}

// BuildWorkspaceSkillSnapshot discovers, filters, and builds a complete snapshot.
func BuildWorkspaceSkillSnapshot(cfg SnapshotConfig) *FullSkillSnapshot {
	// Discover all skills.
	allEntries := DiscoverWorkspaceSkills(cfg.DiscoverConfig)

	// Filter by eligibility.
	eligible := FilterEligibleSkills(allEntries, cfg.Eligibility)

	// Apply name-based filter.
	filtered := FilterBySkillFilter(eligible, cfg.SkillFilter)

	// Separate model-invocation-enabled entries for prompt.
	var promptEntries []SkillEntry
	for _, entry := range filtered {
		if entry.Invocation == nil || !entry.Invocation.DisableModelInvocation {
			promptEntries = append(promptEntries, entry)
		}
	}

	// Convert to PromptSkill and compact paths.
	promptSkills := entriesToPromptSkills(promptEntries)
	promptSkills = CompactSkillPaths(promptSkills)

	// Build prompt with limits.
	limits := cfg.limits()
	result := BuildSkillsPrompt(promptSkills, limits)

	// Assemble final prompt with optional notes.
	truncNote := BuildTruncationNote(result, len(promptSkills))
	parts := []string{}
	if cfg.RemoteNote != "" {
		parts = append(parts, strings.TrimSpace(cfg.RemoteNote))
	}
	if truncNote != "" {
		parts = append(parts, truncNote)
	}
	if result.Prompt != "" {
		parts = append(parts, result.Prompt)
	}
	finalPrompt := strings.Join(parts, "\n")

	// Build skill summaries.
	summaries := make([]SkillSummary, 0, len(filtered))
	for _, entry := range filtered {
		s := SkillSummary{Name: entry.Skill.Name}
		if entry.Metadata != nil {
			s.PrimaryEnv = entry.Metadata.PrimaryEnv
			if entry.Metadata.Requires != nil && len(entry.Metadata.Requires.Env) > 0 {
				s.RequiredEnv = append([]string{}, entry.Metadata.Requires.Env...)
			}
		}
		summaries = append(summaries, s)
	}

	// Build resolved skills (canonical paths, not compacted).
	resolvedSkills := entriesToPromptSkills(promptEntries)

	normalizedFilter := NormalizeSkillFilter(cfg.SkillFilter)

	return &FullSkillSnapshot{
		Prompt:         finalPrompt,
		Skills:         summaries,
		SkillFilter:    normalizedFilter,
		ResolvedSkills: resolvedSkills,
		Version:        cfg.SnapshotVersion,
	}
}

func entriesToPromptSkills(entries []SkillEntry) []PromptSkill {
	result := make([]PromptSkill, 0, len(entries))
	for _, entry := range entries {
		disableModel := false
		if entry.Invocation != nil {
			disableModel = entry.Invocation.DisableModelInvocation
		}
		result = append(result, PromptSkill{
			Name:                   entry.Skill.Name,
			Description:            entry.Skill.Description,
			FilePath:               entry.Skill.FilePath,
			DisableModelInvocation: disableModel,
		})
	}
	return result
}
