// prompt.go builds the LLM system prompt for available skills.
//
// Originally ported formatSkillsCompact from the retired TypeScript skills package,
// applySkillsPromptLimits(), and the pi-coding-agent formatSkillsForPrompt().
// Supports full and compact tool responses plus a name-only ambient manifest,
// all with budget enforcement.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptSkill is the input type for prompt building.
type PromptSkill struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	FilePath    string    `json:"filePath"`
	Category    string    `json:"category,omitempty"`
	Version     string    `json:"version,omitempty"`
	Type        SkillType `json:"type,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	// Triggers are NOT rendered into any prompt block (the semi-static skills
	// index stays byte-stable) — they feed the chat pipeline's deterministic
	// per-turn auto-hint matcher (chat/skill_hints.go).
	Triggers      []string `json:"triggers,omitempty"`
	RelatedSkills []string `json:"relatedSkills,omitempty"`
	// RequiresTools is likewise NOT rendered into any prompt block — it feeds
	// the chat pipeline's skill-consult auto-activation: reading a skill body
	// activates its required deferred tools in the same step, so the skill's
	// instructions and tools arrive as one bundle
	// (chat/tool_skill_required_tools.go).
	RequiresTools []string `json:"requiresTools,omitempty"`
	// ExerciseTools feeds usage attribution only (never eligibility).
	ExerciseTools          []string `json:"exerciseTools,omitempty"`
	DisableModelInvocation bool     `json:"disableModelInvocation,omitempty"`
	// Body is retained in the in-memory snapshot for exact-trigger JIT
	// injection. It is never serialized or rendered in the ambient catalog.
	Body string `json:"-"`
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
	return buildBudgetedSkills(visibleSkills(skills), limits, formatSkillsFull, formatSkillsCompact)
}

// formatSkillsFull renders the full skills prompt with name, description, and file path.
// Matches the output of pi-coding-agent's formatSkillsForPrompt().
func formatSkillsFull(skills []PromptSkill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n<available_skills>")

	for _, s := range skills {
		b.WriteString("\n  <skill>")
		b.WriteString("\n    <name>")
		b.WriteString(escapeXML(s.Name))
		b.WriteString("</name>")
		if s.Category != "" {
			b.WriteString("\n    <category>")
			b.WriteString(escapeXML(s.Category))
			b.WriteString("</category>")
		}
		if s.Description != "" {
			b.WriteString("\n    <description>")
			b.WriteString(escapeXML(s.Description))
			b.WriteString("</description>")
		}
		if len(s.Tags) > 0 {
			b.WriteString("\n    <tags>")
			b.WriteString(escapeXML(strings.Join(s.Tags, ", ")))
			b.WriteString("</tags>")
		}
		if len(s.RelatedSkills) > 0 {
			b.WriteString("\n    <related_skills>")
			b.WriteString(escapeXML(strings.Join(s.RelatedSkills, ", ")))
			b.WriteString("</related_skills>")
		}
		b.WriteString("\n    <location>")
		b.WriteString(escapeXML(s.FilePath))
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
	b.WriteString("\n\n<available_skills>")

	for _, s := range skills {
		b.WriteString("\n  <skill>")
		b.WriteString("\n    <name>")
		b.WriteString(escapeXML(s.Name))
		b.WriteString("</name>")
		b.WriteString("\n    <location>")
		b.WriteString(escapeXML(s.FilePath))
		b.WriteString("</location>")
		b.WriteString("\n  </skill>")
	}

	b.WriteString("\n</available_skills>")
	return b.String()
}

// formatSkillsNameIndex renders the ambient catalog as names only. Purpose,
// location and procedure arrive just in time through exact-trigger context or
// skills(action=list/read). Paying every skill's description and path on every
// turn made the semi-static block grow linearly with the catalog even though a
// turn normally consults zero or one skill.
func formatSkillsNameIndex(skills []PromptSkill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n<available_skills>")

	for _, s := range skills {
		b.WriteString("\n- ")
		b.WriteString(indexLineText(s.Name))
	}

	b.WriteString("\n</available_skills>")
	return b.String()
}

// indexLineText flattens a frontmatter-sourced value onto one line so a
// multi-line name cannot break the one-line-per-skill index shape.
func indexLineText(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}

// BuildSkillsIndex builds the ambient name-only manifest. Descriptions and
// paths are deliberately excluded even when there is ample budget: exact
// trigger matches carry a top-k summary in the per-turn tail, while unmatched
// complex work queries the skills tool on demand. The shared budget helper
// still enforces count/byte limits and prefix truncation for very large catalogs.
func BuildSkillsIndex(skills []PromptSkill, limits SkillsLimits) PromptResult {
	visible := visibleSkills(skills)
	return buildBudgetedSkills(visible, limits, formatSkillsNameIndex, nil)
}

func visibleSkills(skills []PromptSkill) []PromptSkill {
	visible := make([]PromptSkill, 0, len(skills))
	for _, skill := range skills {
		if !skill.DisableModelInvocation {
			visible = append(visible, skill)
		}
	}
	return visible
}

func buildBudgetedSkills(
	visible []PromptSkill,
	limits SkillsLimits,
	fullFormat func([]PromptSkill) string,
	compactFormat func([]PromptSkill) string,
) PromptResult {
	if len(visible) == 0 {
		return PromptResult{}
	}

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

	if prompt := fullFormat(visible); len(prompt) <= maxChars {
		return PromptResult{
			Prompt:    prompt,
			Truncated: truncated,
			Compact:   false,
			Count:     len(visible),
		}
	}

	budgetFormat := fullFormat
	compact := false
	compactBudget := maxChars - compactWarningOverhead
	if compactFormat != nil {
		if prompt := compactFormat(visible); len(prompt) <= compactBudget {
			return PromptResult{
				Prompt:    prompt,
				Truncated: truncated,
				Compact:   true,
				Count:     len(visible),
			}
		}
		budgetFormat = compactFormat
		compact = true
	}

	lo, hi := 0, len(visible)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if len(budgetFormat(visible[:mid])) <= compactBudget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	visible = visible[:lo]
	truncated = true

	return PromptResult{
		Prompt:    budgetFormat(visible),
		Truncated: truncated,
		Compact:   compact,
		Count:     len(visible),
	}
}

func escapeXML(s string) string {
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
	fmt.Fprintf(&b, "%d skills available. Use `read` to load a skill's SKILL.md when needed.\n\n", len(filtered))
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
