// commands.go builds slash command specifications from skill entries.
//
// This ports buildWorkspaceSkillCommandSpecs() from src/agents/skills/workspace.ts.
package skills

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	skillCommandMaxLength            = 32
	skillCommandFallback             = "skill"
	skillCommandDescriptionMaxLength = 100
)

var nonAlphanumericPattern = regexp.MustCompile(`[^a-z0-9_]+`)
var multiUnderscorePattern = regexp.MustCompile(`_+`)
var leadingTrailingUnderscore = regexp.MustCompile(`^_+|_+$`)

// SkillCommandSpec represents a slash command derived from a skill.
type SkillCommandSpec struct {
	Name        string                    `json:"name"`
	SkillName   string                    `json:"skillName"`
	Description string                    `json:"description"`
	Dispatch    *SkillCommandDispatchSpec `json:"dispatch,omitempty"`
}

// SkillCommandDispatchSpec describes how to dispatch a skill command invocation.
type SkillCommandDispatchSpec struct {
	Kind     string `json:"kind"` // "tool"
	ToolName string `json:"toolName"`
	ArgMode  string `json:"argMode,omitempty"` // "raw"
}

// BuildSkillCommandSpecs builds slash command specs from eligible skill entries.
func BuildSkillCommandSpecs(entries []SkillEntry, reserved map[string]bool) []SkillCommandSpec {
	used := make(map[string]bool)
	for name := range reserved {
		used[strings.ToLower(name)] = true
	}

	var specs []SkillCommandSpec
	for _, entry := range entries {
		// Only user-invocable skills get commands.
		if entry.Invocation != nil && !entry.Invocation.UserInvocable {
			continue
		}

		rawName := entry.Skill.Name
		base := sanitizeSkillCommandName(rawName)
		unique := resolveUniqueSkillCommandName(base, used)
		used[strings.ToLower(unique)] = true

		rawDesc := entry.Skill.Description
		if rawDesc == "" {
			rawDesc = rawName
		}
		desc := rawDesc
		if utf8.RuneCountInString(desc) > skillCommandDescriptionMaxLength {
			runes := []rune(desc)
			desc = string(runes[:skillCommandDescriptionMaxLength-1]) + "…"
		}

		dispatch := resolveCommandDispatch(entry)

		spec := SkillCommandSpec{
			Name:        unique,
			SkillName:   rawName,
			Description: desc,
		}
		if dispatch != nil {
			spec.Dispatch = dispatch
		}
		specs = append(specs, spec)
	}
	return specs
}

func sanitizeSkillCommandName(raw string) string {
	normalized := strings.ToLower(raw)
	normalized = nonAlphanumericPattern.ReplaceAllString(normalized, "_")
	normalized = multiUnderscorePattern.ReplaceAllString(normalized, "_")
	normalized = leadingTrailingUnderscore.ReplaceAllString(normalized, "")
	if len(normalized) > skillCommandMaxLength {
		normalized = normalized[:skillCommandMaxLength]
	}
	if normalized == "" {
		return skillCommandFallback
	}
	return normalized
}

func resolveUniqueSkillCommandName(base string, used map[string]bool) string {
	normalizedBase := strings.ToLower(base)
	if !used[normalizedBase] {
		return base
	}
	for i := 2; i < 1000; i++ {
		suffix := fmt.Sprintf("_%d", i)
		maxBase := skillCommandMaxLength - len(suffix)
		if maxBase < 1 {
			maxBase = 1
		}
		trimmedBase := base
		if len(trimmedBase) > maxBase {
			trimmedBase = trimmedBase[:maxBase]
		}
		candidate := trimmedBase + suffix
		if !used[strings.ToLower(candidate)] {
			return candidate
		}
	}
	maxBase := skillCommandMaxLength - 2
	if maxBase < 1 {
		maxBase = 1
	}
	fallback := base
	if len(fallback) > maxBase {
		fallback = fallback[:maxBase]
	}
	return fallback + "_x"
}

func resolveCommandDispatch(entry SkillEntry) *SkillCommandDispatchSpec {
	fm := entry.Frontmatter
	if fm == nil {
		return nil
	}

	kindRaw := strings.ToLower(strings.TrimSpace(
		fmGetAny(fm, "command-dispatch", "command_dispatch"),
	))
	if kindRaw == "" || kindRaw != "tool" {
		return nil
	}

	toolName := strings.TrimSpace(
		fmGetAny(fm, "command-tool", "command_tool"),
	)
	if toolName == "" {
		return nil
	}

	argModeRaw := strings.ToLower(strings.TrimSpace(
		fmGetAny(fm, "command-arg-mode", "command_arg_mode"),
	))
	argMode := "raw"
	if argModeRaw != "" && argModeRaw != "raw" {
		argMode = "raw" // fallback to raw for unknown modes
	}

	return &SkillCommandDispatchSpec{
		Kind:     "tool",
		ToolName: toolName,
		ArgMode:  argMode,
	}
}

func fmGetAny(fm ParsedFrontmatter, keys ...string) string {
	for _, key := range keys {
		if v, ok := fm[key]; ok && v != "" {
			return v
		}
	}
	return ""
}
