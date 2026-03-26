// status.go — Status message builder.
// Mirrors src/auto-reply/status.ts (866 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"fmt"
	"strings"
)

// StatusReport holds all data for building a status message.
type StatusReport struct {
	SessionKey      string
	AgentID         string
	Model           string
	Provider        string
	Channel         string
	IsGroup         bool
	ThinkLevel      types.ThinkLevel
	FastMode        bool
	VerboseLevel    types.VerboseLevel
	ReasoningLevel  types.ReasoningLevel
	ElevatedLevel   types.ElevatedLevel
	SendPolicy      string
	GroupActivation types.GroupActivationMode
	Usage           *SessionUsage
	RunCount        int
}

// BuildStatusMessage creates a formatted status message from a report.
func BuildStatusMessage(report StatusReport) string {
	var sections []string

	// Session section.
	sections = append(sections, fmt.Sprintf("📋 **Session:** `%s`", report.SessionKey))

	// Model section.
	if report.Model != "" {
		ref := FormatProviderModelRef(report.Provider, report.Model)
		sections = append(sections, fmt.Sprintf("🤖 **Model:** %s", ref))
	}

	// Mode settings.
	var modes []string
	if report.ThinkLevel != "" && report.ThinkLevel != types.ThinkOff {
		modes = append(modes, fmt.Sprintf("Think: %s", report.ThinkLevel))
	}
	if report.FastMode {
		modes = append(modes, "Fast: on")
	}
	if report.VerboseLevel != "" && report.VerboseLevel != types.VerboseOff {
		modes = append(modes, fmt.Sprintf("Verbose: %s", report.VerboseLevel))
	}
	if report.ReasoningLevel != "" && report.ReasoningLevel != types.ReasoningOff {
		modes = append(modes, fmt.Sprintf("Reasoning: %s", report.ReasoningLevel))
	}
	if report.ElevatedLevel != "" && report.ElevatedLevel != types.ElevatedOff {
		modes = append(modes, fmt.Sprintf("Elevated: %s", report.ElevatedLevel))
	}
	if len(modes) > 0 {
		sections = append(sections, "⚙️ **Modes:** "+strings.Join(modes, " | "))
	}

	// Channel section.
	if report.Channel != "" {
		sections = append(sections, fmt.Sprintf("📡 **Channel:** %s", report.Channel))
	}
	if report.IsGroup {
		activation := "mention"
		if report.GroupActivation != "" {
			activation = string(report.GroupActivation)
		}
		sections = append(sections, fmt.Sprintf("👥 **Group activation:** %s", activation))
	}

	// Usage section.
	if report.Usage != nil && report.Usage.TotalTokens > 0 {
		sections = append(sections, fmt.Sprintf("📊 **Usage:** %s", report.Usage.FormatUsage()))
	}

	return strings.Join(sections, "\n")
}

// BuildHelpMessage creates a help/commands message.
func BuildHelpMessage(commands []ChatCommandDefinition) string {
	var sections []string
	sections = append(sections, "**Available Commands:**\n")

	// Group by category.
	categories := map[CommandCategory][]ChatCommandDefinition{}
	for _, cmd := range commands {
		cat := cmd.Category
		if cat == "" {
			cat = "other"
		}
		categories[cat] = append(categories[cat], cmd)
	}

	categoryOrder := []CommandCategory{
		CategorySession, CategoryOptions, CategoryStatus,
		CategoryManagement, CategoryMedia, CategoryTools, CategoryDocks,
		"other",
	}

	for _, cat := range categoryOrder {
		cmds, ok := categories[cat]
		if !ok || len(cmds) == 0 {
			continue
		}
		sections = append(sections, fmt.Sprintf("**%s:**", formatCategoryLabel(cat)))
		for _, cmd := range cmds {
			alias := cmd.Key
			if len(cmd.TextAliases) > 0 {
				alias = cmd.TextAliases[0]
			}
			sections = append(sections, fmt.Sprintf("  `%s` — %s", alias, cmd.Description))
		}
		sections = append(sections, "")
	}

	return strings.Join(sections, "\n")
}

func formatCategoryLabel(cat CommandCategory) string {
	switch cat {
	case CategorySession:
		return "Session"
	case CategoryOptions:
		return "Options"
	case CategoryStatus:
		return "Status"
	case CategoryManagement:
		return "Management"
	case CategoryMedia:
		return "Media"
	case CategoryTools:
		return "Tools"
	case CategoryDocks:
		return "Docks"
	default:
		return "Other"
	}
}

// FormatTokenCount formats a token count with commas.
func FormatTokenCount(tokens int64) string {
	if tokens == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", tokens)
	if len(s) <= 3 {
		return s
	}
	// Insert commas.
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// FormatContextUsageShort returns a compact context usage string.
func FormatContextUsageShort(used, total int) string {
	if total == 0 {
		return ""
	}
	pct := float64(used) / float64(total) * 100
	return fmt.Sprintf("%s / %s (%.0f%%)",
		FormatTokenCount(int64(used)),
		FormatTokenCount(int64(total)),
		pct)
}

// BuildCommandsMessage creates a paginated commands listing.
func BuildCommandsMessage(commands []ChatCommandDefinition, page, perPage int) string {
	if perPage <= 0 {
		perPage = 20
	}
	start := page * perPage
	if start >= len(commands) {
		return "No more commands."
	}
	end := start + perPage
	if end > len(commands) {
		end = len(commands)
	}

	var lines []string
	for _, cmd := range commands[start:end] {
		alias := cmd.Key
		if len(cmd.TextAliases) > 0 {
			alias = cmd.TextAliases[0]
		}
		lines = append(lines, fmt.Sprintf("`%s` — %s", alias, cmd.Description))
	}

	if end < len(commands) {
		lines = append(lines, fmt.Sprintf("\n_Showing %d-%d of %d. Use /help %d for more._",
			start+1, end, len(commands), page+1))
	}

	return strings.Join(lines, "\n")
}
