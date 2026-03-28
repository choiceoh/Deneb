// commands_plugins.go — Plugin management command handler.
// Mirrors src/auto-reply/reply/commands-plugins.ts (275 LOC).
package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// PluginCommandAction identifies the action of a /plugins command.
type PluginCommandAction string

const (
	PluginActionList    PluginCommandAction = "list"
	PluginActionInspect PluginCommandAction = "inspect"
	PluginActionEnable  PluginCommandAction = "enable"
	PluginActionDisable PluginCommandAction = "disable"
	PluginActionError   PluginCommandAction = "error"
)

// PluginCommand represents a parsed /plugins command.
type PluginCommand struct {
	Action  PluginCommandAction
	Name    string // plugin name for inspect/enable/disable
	Message string // error message
}

var pluginsCommandRe = regexp.MustCompile(`(?i)^/plugins?(?:\s+|:\s*)(.*)$`)

// ParsePluginsCommand parses a /plugins command from raw text.
// Returns nil if the text is not a /plugins command.
func ParsePluginsCommand(raw string) *PluginCommand {
	trimmed := strings.TrimSpace(raw)
	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, "/plugins") && !strings.HasPrefix(lowered, "/plugin") {
		return nil
	}

	// Exact match: /plugins or /plugin.
	if lowered == "/plugins" || lowered == "/plugin" {
		return &PluginCommand{Action: PluginActionList}
	}

	m := pluginsCommandRe.FindStringSubmatch(trimmed)
	var args string
	if m != nil {
		args = strings.TrimSpace(m[1])
	}
	if args == "" {
		return &PluginCommand{Action: PluginActionList}
	}

	fields := strings.SplitN(args, " ", 2)
	action := strings.ToLower(fields[0])
	name := ""
	if len(fields) > 1 {
		name = strings.TrimSpace(fields[1])
	}

	switch action {
	case "list", "ls":
		return &PluginCommand{Action: PluginActionList}

	case "inspect", "info", "show":
		return &PluginCommand{Action: PluginActionInspect, Name: name}

	case "enable", "on":
		if name == "" {
			return &PluginCommand{
				Action:  PluginActionError,
				Message: "Usage: /plugins enable <name>",
			}
		}
		return &PluginCommand{Action: PluginActionEnable, Name: name}

	case "disable", "off":
		if name == "" {
			return &PluginCommand{
				Action:  PluginActionError,
				Message: "Usage: /plugins disable <name>",
			}
		}
		return &PluginCommand{Action: PluginActionDisable, Name: name}

	default:
		// Treat unknown action as inspect with name.
		return &PluginCommand{Action: PluginActionInspect, Name: args}
	}
}

// PluginRecord describes a discovered plugin.
type PluginRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"` // "loaded", "disabled", "error", "not_found"
	Format       string `json:"format,omitempty"`
	BundleFormat string `json:"bundleFormat,omitempty"`
	Version      string `json:"version,omitempty"`
}

// PluginStatusReport holds the status of all discovered plugins.
type PluginStatusReport struct {
	WorkspaceDir string         `json:"workspaceDir"`
	Plugins      []PluginRecord `json:"plugins"`
}

// FormatPluginLabel formats a plugin display name.
func FormatPluginLabel(plugin PluginRecord) string {
	if plugin.Name == "" || plugin.Name == plugin.ID {
		return plugin.ID
	}
	return fmt.Sprintf("%s (%s)", plugin.Name, plugin.ID)
}

// FormatPluginsList creates a list summary of all plugins.
func FormatPluginsList(report PluginStatusReport) string {
	if len(report.Plugins) == 0 {
		workspace := report.WorkspaceDir
		if workspace == "" {
			workspace = "(unknown workspace)"
		}
		return fmt.Sprintf("🔌 No plugins found for workspace %s.", workspace)
	}

	loaded := 0
	for _, p := range report.Plugins {
		if p.Status == "loaded" {
			loaded++
		}
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("🔌 Plugins (%d/%d loaded)", loaded, len(report.Plugins)))
	for _, p := range report.Plugins {
		format := p.Format
		if format == "" {
			format = "deneb"
		}
		if p.BundleFormat != "" {
			format = format + "/" + p.BundleFormat
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", FormatPluginLabel(p), p.Status, format))
	}
	return strings.Join(lines, "\n")
}

// FindPlugin looks up a plugin by ID or name (case-insensitive).
func FindPlugin(report PluginStatusReport, rawName string) *PluginRecord {
	target := strings.ToLower(strings.TrimSpace(rawName))
	if target == "" {
		return nil
	}
	for _, p := range report.Plugins {
		if strings.ToLower(p.ID) == target || strings.ToLower(p.Name) == target {
			return &p
		}
	}
	return nil
}

// RenderJSONBlock formats a value as a markdown JSON code block.
func RenderJSONBlock(label string, value any) string {
	b, _ := json.MarshalIndent(value, "", "  ")
	return fmt.Sprintf("%s\n```json\n%s\n```", label, string(b))
}

// HandlePluginsCommandResult holds the result of handling a /plugins command.
type HandlePluginsCommandResult struct {
	ShouldContinue bool
	ReplyText      string
}

// HandlePluginsCommand processes a /plugins command.
// The caller must supply the plugin status report (from the plugin subsystem).
func HandlePluginsCommand(cmd PluginCommand, report PluginStatusReport) HandlePluginsCommandResult {
	switch cmd.Action {
	case PluginActionError:
		return HandlePluginsCommandResult{
			ReplyText: "⚠️ " + cmd.Message,
		}

	case PluginActionList:
		return HandlePluginsCommandResult{
			ReplyText: FormatPluginsList(report),
		}

	case PluginActionInspect:
		if cmd.Name == "" {
			return HandlePluginsCommandResult{
				ReplyText: FormatPluginsList(report),
			}
		}
		if strings.ToLower(cmd.Name) == "all" {
			return HandlePluginsCommandResult{
				ReplyText: RenderJSONBlock("🔌 Plugins", report.Plugins),
			}
		}
		plugin := FindPlugin(report, cmd.Name)
		if plugin == nil {
			return HandlePluginsCommandResult{
				ReplyText: fmt.Sprintf("🔌 No plugin named %q found.", cmd.Name),
			}
		}
		return HandlePluginsCommandResult{
			ReplyText: RenderJSONBlock(fmt.Sprintf("🔌 Plugin %q", plugin.ID), plugin),
		}

	case PluginActionEnable, PluginActionDisable:
		plugin := FindPlugin(report, cmd.Name)
		if plugin == nil {
			return HandlePluginsCommandResult{
				ReplyText: fmt.Sprintf("🔌 No plugin named %q found.", cmd.Name),
			}
		}
		action := "enabled"
		if cmd.Action == PluginActionDisable {
			action = "disabled"
		}
		return HandlePluginsCommandResult{
			ReplyText: fmt.Sprintf("🔌 Plugin %q %s. Restart the gateway to apply.", plugin.ID, action),
		}
	}

	return HandlePluginsCommandResult{}
}
