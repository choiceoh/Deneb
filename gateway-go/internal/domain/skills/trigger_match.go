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
			if length < 2 || length <= best || !triggerOccurs(message, trigger) {
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

// triggerOccurs reports whether trigger appears in message as a real token.
//
// Korean has no word delimiters, so substring containment is the only workable
// test there and stays as-is. ASCII triggers are different: a short latin
// trigger matches INSIDE unrelated words — the live corpus fired
// contract-review's "mou" on "cumulative amount" five times in 30 days, each
// injecting its 8KB body into an unrelated turn. For an all-ASCII trigger the
// match must therefore sit on word boundaries.
func triggerOccurs(message, trigger string) bool {
	if !isASCIIWordTrigger(trigger) {
		return strings.Contains(message, trigger)
	}
	for offset := 0; ; {
		i := strings.Index(message[offset:], trigger)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(trigger)
		if !asciiWordByte(message, start-1) && !asciiWordByte(message, end) {
			return true
		}
		offset = start + 1
		if offset >= len(message) {
			return false
		}
	}
}

// isASCIIWordTrigger reports whether every rune is an ASCII letter or digit —
// the class that can hide inside a longer latin word.
func isASCIIWordTrigger(trigger string) bool {
	if trigger == "" {
		return false
	}
	for _, r := range trigger {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// asciiWordByte reports whether the byte at i is an ASCII letter or digit
// (out-of-range counts as a boundary).
func asciiWordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
