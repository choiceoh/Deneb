package skills

import (
	"sort"
	"strings"
)

// MatchSkillTriggers returns the skills whose explicit frontmatter triggers
// occur in message. Longer trigger matches win, with skill name as the stable
// tie-break. A non-positive limit means no cap.
//
// Trigger matching intentionally stays literal and opt-in: descriptions and
// tags contain broad taxonomy words that would create noisy auto-activation.
func MatchSkillTriggers(message string, resolved []PromptSkill, limit int) []PromptSkill {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" || len(resolved) == 0 {
		return nil
	}

	type match struct {
		skill PromptSkill
		score int
	}
	matches := make([]match, 0, len(resolved))
	for _, skill := range resolved {
		if skill.DisableModelInvocation || len(skill.Triggers) == 0 {
			continue
		}
		best := 0
		for _, trigger := range skill.Triggers {
			trigger = strings.ToLower(strings.TrimSpace(trigger))
			length := len([]rune(trigger))
			if length < 2 || length <= best || !strings.Contains(message, trigger) {
				continue
			}
			best = length
		}
		if best > 0 {
			matches = append(matches, match{skill: skill, score: best})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].skill.Name < matches[j].skill.Name
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	out := make([]PromptSkill, 0, len(matches))
	for _, matched := range matches {
		out = append(out, matched.skill)
	}
	return out
}
