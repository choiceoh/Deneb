// status.go builds aggregated status reports for discovered skills.
package skills

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

	binsSet := make(map[string]bool)
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
					binsSet[bin] = true
				}
				for _, bin := range entry.Metadata.Requires.AnyBins {
					binsSet[bin] = true
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
