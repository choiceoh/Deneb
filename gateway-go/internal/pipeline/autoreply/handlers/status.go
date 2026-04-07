// status.go — Status message builder.
// Mirrors src/auto-reply/status.ts (866 LOC).
package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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
	RunCount        int

	// Context usage.
	ContextUsedTokens  int
	ContextTotalTokens int

	// Server-level fields (populated from StatusDeps).
	Version           string
	StartedAt         time.Time
	SessionCount      int
	WSConnections     int32
	ProviderUsage     map[string]*ProviderUsageStats
	ChannelHealth     []ChannelHealthEntry
	LastFailureReason string // reason the most recent run failed, if any
}

// BuildStatusMessage creates a formatted status message from a report.
func BuildStatusMessage(report StatusReport) string {
	var sections []string

	// Session section.
	sections = append(sections, fmt.Sprintf("📋 **Session:** `%s`", report.SessionKey))

	// Model section.
	if report.Model != "" {
		ref := model.FormatProviderModelRef(report.Provider, report.Model)
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

	// Context usage section.
	if report.ContextTotalTokens > 0 {
		sections = append(sections, fmt.Sprintf("📊 **Context:** %s",
			FormatContextUsageShort(report.ContextUsedTokens, report.ContextTotalTokens)))
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

	// Gateway info section.
	if report.Version != "" || !report.StartedAt.IsZero() {
		var gatewayParts []string
		if report.Version != "" {
			gatewayParts = append(gatewayParts, fmt.Sprintf("Gateway v%s", report.Version))
		}
		if !report.StartedAt.IsZero() {
			gatewayParts = append(gatewayParts, fmt.Sprintf("Uptime: %s", formatUptime(time.Since(report.StartedAt))))
		}
		sections = append(sections, "🖥️ "+strings.Join(gatewayParts, " | "))
	}

	// System subsystem line.
	if report.Version != "" || !report.StartedAt.IsZero() {
		sections = append(sections, fmt.Sprintf("🔧 Sessions: %d | WS: %d",
			report.SessionCount, report.WSConnections))
	}

	// Per-provider API usage.
	if len(report.ProviderUsage) > 0 {
		sections = append(sections, buildProviderUsageSection(report.ProviderUsage))
	}

	// Channel health.
	if len(report.ChannelHealth) > 0 {
		for _, ch := range report.ChannelHealth {
			icon := "💚"
			status := "정상"
			if !ch.Healthy {
				icon = "❌"
				status = "비정상"
				if ch.Reason != "" {
					status = ch.Reason
				}
			}
			sections = append(sections, fmt.Sprintf("%s **%s:** %s", icon, ch.ID, status))
		}
	}

	// Last failure reason (if the most recent run ended in error).
	if report.LastFailureReason != "" {
		sections = append(sections, fmt.Sprintf("⚠️ **마지막 오류:** %s", report.LastFailureReason))
	}

	return strings.Join(sections, "\n")
}

// buildProviderUsageSection formats per-provider API usage as a section.
func buildProviderUsageSection(providers map[string]*ProviderUsageStats) string {
	// Sort provider names for stable output.
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := []string{"📊 **API 사용량:**"}
	for _, name := range names {
		p := providers[name]
		total := p.Input + p.Output
		lines = append(lines, fmt.Sprintf("  %s — %s회, %s tokens (%s in, %s out)",
			name,
			FormatTokenCount(p.Calls),
			formatCompactTokens(total),
			formatCompactTokens(p.Input),
			formatCompactTokens(p.Output),
		))
	}
	return strings.Join(lines, "\n")
}

// formatUptime formats a duration as a compact uptime string (e.g. "2d 5h 32m").
func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// formatCompactTokens formats token counts in compact form (e.g. "1.2M", "890K", "500").
func formatCompactTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
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
