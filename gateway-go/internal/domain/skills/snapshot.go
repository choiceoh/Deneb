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
	Category    string   `json:"category,omitempty"`
	Version     string   `json:"version,omitempty"`
	PrimaryEnv  string   `json:"primaryEnv,omitempty"`
	RequiredEnv []string `json:"requiredEnv,omitempty"`
}

// FullSkillSnapshot is the complete result of building a workspace skill snapshot,
// matching the TypeScript SkillSnapshot type.
type FullSkillSnapshot struct {
	Prompt             string         `json:"prompt"`
	Skills             []SkillSummary `json:"skills"`
	SkillFilter        []string       `json:"skillFilter,omitempty"`
	ResolvedSkills     []PromptSkill  `json:"resolvedSkills,omitempty"`
	DiscoverableSkills []PromptSkill  `json:"discoverableSkills,omitempty"` // non-always skills for on-demand listing
	Version            int64          `json:"version,omitempty"`
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
	// Split into always (injected in system prompt) and discoverable (on-demand via skills_list tool).
	var alwaysEntries, discoverableEntries []SkillEntry
	for _, entry := range filtered {
		if entry.Invocation != nil && entry.Invocation.DisableModelInvocation {
			continue
		}
		if entry.Metadata != nil && entry.Metadata.Always {
			alwaysEntries = append(alwaysEntries, entry)
		} else {
			discoverableEntries = append(discoverableEntries, entry)
		}
	}

	// Only always-skills go into the system prompt.
	promptSkills := entriesToPromptSkills(alwaysEntries)
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
		s := SkillSummary{
			Name:     entry.Skill.Name,
			Category: entry.Skill.Category,
			Version:  entry.Skill.Version,
		}
		if entry.Metadata != nil {
			s.PrimaryEnv = entry.Metadata.PrimaryEnv
			if entry.Metadata.Requires != nil && len(entry.Metadata.Requires.Env) > 0 {
				s.RequiredEnv = append([]string{}, entry.Metadata.Requires.Env...)
			}
		}
		summaries = append(summaries, s)
	}

	// Build resolved skills (canonical paths, not compacted) — includes both always and discoverable.
	allPromptEntries := make([]SkillEntry, 0, len(alwaysEntries)+len(discoverableEntries))
	allPromptEntries = append(allPromptEntries, alwaysEntries...)
	allPromptEntries = append(allPromptEntries, discoverableEntries...)
	resolvedSkills := entriesToPromptSkills(allPromptEntries)

	// Build discoverable skills list (compacted paths for tool responses).
	discoverableSkills := entriesToPromptSkills(discoverableEntries)
	discoverableSkills = CompactSkillPaths(discoverableSkills)

	normalizedFilter := NormalizeSkillFilter(cfg.SkillFilter)

	return &FullSkillSnapshot{
		Prompt:             finalPrompt,
		Skills:             summaries,
		SkillFilter:        normalizedFilter,
		ResolvedSkills:     resolvedSkills,
		DiscoverableSkills: discoverableSkills,
		Version:            cfg.SnapshotVersion,
	}
}

func entriesToPromptSkills(entries []SkillEntry) []PromptSkill {
	result := make([]PromptSkill, 0, len(entries))
	for _, entry := range entries {
		disableModel := false
		if entry.Invocation != nil {
			disableModel = entry.Invocation.DisableModelInvocation
		}
		ps := PromptSkill{
			Name:                   entry.Skill.Name,
			Description:            entry.Skill.Description,
			FilePath:               entry.Skill.FilePath,
			Category:               entry.Skill.Category,
			Version:                entry.Skill.Version,
			Type:                   entry.Skill.Type,
			DisableModelInvocation: disableModel,
		}
		if entry.Metadata != nil {
			ps.Tags = entry.Metadata.Tags
			ps.RelatedSkills = entry.Metadata.RelatedSkills
		}
		result = append(result, ps)
	}
	return result
}
