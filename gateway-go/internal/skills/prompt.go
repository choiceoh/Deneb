// prompt.go builds the LLM system prompt for available skills.
//
// This ports src/agents/skills/workspace.ts:formatSkillsCompact(),
// applySkillsPromptLimits(), and the pi-coding-agent formatSkillsForPrompt().
// Supports full format (name + description + location) and compact format
// (name + location only) with budget-aware fallback.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptSkill is the input type for prompt building.
type PromptSkill struct {
	Name                   string    `json:"name"`
	Description            string    `json:"description,omitempty"`
	FilePath               string    `json:"filePath"`
	Category               string    `json:"category,omitempty"`
	Version                string    `json:"version,omitempty"`
	Type                   SkillType `json:"type,omitempty"`
	Tags                   []string  `json:"tags,omitempty"`
	RelatedSkills          []string  `json:"relatedSkills,omitempty"`
	DisableModelInvocation bool      `json:"disableModelInvocation,omitempty"`
}

// PromptResult is the output of prompt building.
type PromptResult struct {
	Prompt    string `json:"prompt"`
	Truncated bool   `json:"truncated"`
	Compact   bool   `json:"compact"`
	Count     int    `json:"count"`
}

// compactWarningOverhead is the character budget reserved for the compact-mode warning line.
const compactWarningOverhead = 150

// BuildSkillsPrompt builds the formatted skills prompt with budget enforcement.
// Returns full format if it fits, compact format as fallback, with binary search
// for largest fitting subset if compact also exceeds the budget.
//
// Prompt cache design: skills are rendered in the semi-static block of the system
// prompt with Anthropic ephemeral cache control. Stable ordering (sorted by name
// from discovery) ensures cache hits across turns. Only SKILL.md file changes
// invalidate the skills cache — not conversation state or tool results.
func BuildSkillsPrompt(skills []PromptSkill, limits SkillsLimits) PromptResult {
	// Filter out model-invocation-disabled skills.
	var visible []PromptSkill
	for _, s := range skills {
		if !s.DisableModelInvocation {
			visible = append(visible, s)
		}
	}

	if len(visible) == 0 {
		return PromptResult{}
	}

	// Apply count limit.
	maxCount := limits.MaxSkillsInPrompt
	if maxCount <= 0 {
		maxCount = 150
	}
	maxChars := limits.MaxSkillsPromptChars
	if maxChars <= 0 {
		maxChars = 30_000
	}

	truncated := len(visible) > maxCount
	if len(visible) > maxCount {
		visible = visible[:maxCount]
	}

	// Try full format first.
	if len(formatSkillsFull(visible)) <= maxChars {
		return PromptResult{
			Prompt:    formatSkillsFull(visible),
			Truncated: truncated,
			Compact:   false,
			Count:     len(visible),
		}
	}

	// Full format exceeds budget. Try compact format.
	compactBudget := maxChars - compactWarningOverhead
	if len(formatSkillsCompact(visible)) <= compactBudget {
		return PromptResult{
			Prompt:    formatSkillsCompact(visible),
			Truncated: truncated,
			Compact:   true,
			Count:     len(visible),
		}
	}

	// Compact still too large — binary search for largest fitting prefix.
	lo, hi := 0, len(visible)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if len(formatSkillsCompact(visible[:mid])) <= compactBudget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	visible = visible[:lo]
	truncated = true

	return PromptResult{
		Prompt:    formatSkillsCompact(visible),
		Truncated: truncated,
		Compact:   true,
		Count:     len(visible),
	}
}

// formatSkillsFull renders the full skills prompt with name, description, and file path.
// Matches the output of pi-coding-agent's formatSkillsForPrompt().
func formatSkillsFull(skills []PromptSkill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.")
	b.WriteString("\nUse the read tool to load a skill's file when the task matches its name or description.")
	b.WriteString("\nWhen a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.")
	b.WriteString("\n\n<available_skills>")

	for _, s := range skills {
		b.WriteString("\n  <skill>")
		b.WriteString("\n    <name>")
		b.WriteString(escapeXml(s.Name))
		b.WriteString("</name>")
		if s.Category != "" {
			b.WriteString("\n    <category>")
			b.WriteString(escapeXml(s.Category))
			b.WriteString("</category>")
		}
		if s.Description != "" {
			b.WriteString("\n    <description>")
			b.WriteString(escapeXml(s.Description))
			b.WriteString("</description>")
		}
		if len(s.Tags) > 0 {
			b.WriteString("\n    <tags>")
			b.WriteString(escapeXml(strings.Join(s.Tags, ", ")))
			b.WriteString("</tags>")
		}
		if len(s.RelatedSkills) > 0 {
			b.WriteString("\n    <related_skills>")
			b.WriteString(escapeXml(strings.Join(s.RelatedSkills, ", ")))
			b.WriteString("</related_skills>")
		}
		b.WriteString("\n    <location>")
		b.WriteString(escapeXml(s.FilePath))
		b.WriteString("</location>")
		b.WriteString("\n  </skill>")
	}

	b.WriteString("\n</available_skills>")
	return b.String()
}

// formatSkillsCompact renders a compact skills prompt with name and location only.
// Used as fallback when full format exceeds the character budget.
func formatSkillsCompact(skills []PromptSkill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.")
	b.WriteString("\nUse the read tool to load a skill's file when the task matches its name.")
	b.WriteString("\nWhen a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.")
	b.WriteString("\n\n<available_skills>")

	for _, s := range skills {
		b.WriteString("\n  <skill>")
		b.WriteString("\n    <name>")
		b.WriteString(escapeXml(s.Name))
		b.WriteString("</name>")
		b.WriteString("\n    <location>")
		b.WriteString(escapeXml(s.FilePath))
		b.WriteString("</location>")
		b.WriteString("\n  </skill>")
	}

	b.WriteString("\n</available_skills>")
	return b.String()
}

func escapeXml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// CompactSkillPaths replaces the user's home directory prefix with ~/ in file paths
// to reduce system prompt token usage.
func CompactSkillPaths(skills []PromptSkill) []PromptSkill {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return skills
	}
	prefix := home
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	result := make([]PromptSkill, len(skills))
	for i, s := range skills {
		result[i] = s
		if strings.HasPrefix(s.FilePath, prefix) {
			result[i].FilePath = "~/" + s.FilePath[len(prefix):]
		}
	}
	return result
}

// FormatSkillsListResponse formats discoverable skills for the skills_list tool response.
// Supports optional query and category filters.
func FormatSkillsListResponse(skills []PromptSkill, query, category string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	category = strings.ToLower(strings.TrimSpace(category))

	var filtered []PromptSkill
	for _, s := range skills {
		if category != "" && strings.ToLower(s.Category) != category {
			continue
		}
		if query != "" {
			nameMatch := strings.Contains(strings.ToLower(s.Name), query)
			descMatch := strings.Contains(strings.ToLower(s.Description), query)
			catMatch := strings.Contains(strings.ToLower(s.Category), query)
			tagMatch := matchAnyTag(s.Tags, query)
			if !nameMatch && !descMatch && !catMatch && !tagMatch {
				continue
			}
		}
		filtered = append(filtered, s)
	}

	if len(filtered) == 0 {
		if query != "" || category != "" {
			return "No skills match the given filter."
		}
		return "No discoverable skills available."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d skills available. Use `read` to load a skill's SKILL.md when needed.\n\n", len(filtered)))
	for _, s := range filtered {
		b.WriteString("- **")
		b.WriteString(s.Name)
		b.WriteString("**")
		if s.Category != "" {
			b.WriteString(" [")
			b.WriteString(s.Category)
			b.WriteString("]")
		}
		if s.Description != "" {
			b.WriteString(": ")
			b.WriteString(s.Description)
		}
		if len(s.Tags) > 0 {
			b.WriteString("\n  tags: ")
			b.WriteString(strings.Join(s.Tags, ", "))
		}
		if len(s.RelatedSkills) > 0 {
			b.WriteString("\n  related: ")
			b.WriteString(strings.Join(s.RelatedSkills, ", "))
		}
		b.WriteString("\n  → ")
		b.WriteString(s.FilePath)
		b.WriteString("\n")
	}
	return b.String()
}

// matchAnyTag checks if any tag contains the query string (case-insensitive).
func matchAnyTag(tags []string, query string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

// BuildTruncationNote generates the truncation/compact warning message.
func BuildTruncationNote(result PromptResult, totalEligible int) string {
	if result.Truncated {
		if result.Compact {
			return fmt.Sprintf(
				"⚠️ Skills truncated: included %d of %d (compact format, descriptions omitted). Run `deneb skills check` to audit.",
				result.Count, totalEligible,
			)
		}
		return fmt.Sprintf(
			"⚠️ Skills truncated: included %d of %d. Run `deneb skills check` to audit.",
			result.Count, totalEligible,
		)
	}
	if result.Compact {
		return "⚠️ Skills catalog using compact format (descriptions omitted). Run `deneb skills check` to audit."
	}
	return ""
}
