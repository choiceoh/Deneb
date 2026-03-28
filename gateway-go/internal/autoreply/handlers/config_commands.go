// config_commands.go — /config command parsing.
// Mirrors src/auto-reply/reply/config-commands.ts (23 LOC).
//
// Uses ParseSlashCommandOrNull from commands_slash_parse.go and
// ParseConfigValue from config_value.go (both from main).
package handlers

import (
	"fmt"
)

// ConfigCommandAction identifies the action of a /config command.
type ConfigCommandAction string

const (
	ConfigActionShow  ConfigCommandAction = "show"
	ConfigActionSet   ConfigCommandAction = "set"
	ConfigActionUnset ConfigCommandAction = "unset"
	ConfigActionError ConfigCommandAction = "error"
)

// ConfigCommand represents a parsed /config command.
type ConfigCommand struct {
	Action  ConfigCommandAction
	Path    string
	Value   any
	Message string // error message when Action == ConfigActionError
}

// ParseConfigCommand parses a /config command from raw text.
// Returns nil if the text is not a /config command.
func ParseConfigCommand(raw string) *ConfigCommand {
	parsed := ParseSlashCommandOrNull(raw, "/config", "Invalid /config syntax.", "show")
	if parsed == nil {
		return nil
	}
	if !parsed.OK {
		return &ConfigCommand{Action: ConfigActionError, Message: parsed.Message}
	}

	switch parsed.Action {
	case "show", "get":
		return &ConfigCommand{Action: ConfigActionShow, Path: parsed.Args}

	case "unset":
		if parsed.Args == "" {
			return &ConfigCommand{Action: ConfigActionError, Message: "Usage: /config unset <path>"}
		}
		return &ConfigCommand{Action: ConfigActionUnset, Path: parsed.Args}

	case "set":
		return parseConfigSetCommand("/config", parsed.Args)

	default:
		return &ConfigCommand{Action: ConfigActionError, Message: "Usage: /config show|set|unset"}
	}
}

// parseConfigSetCommand handles /config set <path>=<value> or /config set <path> <value>.
func parseConfigSetCommand(slash, args string) *ConfigCommand {
	if args == "" {
		return &ConfigCommand{Action: ConfigActionError, Message: fmt.Sprintf("Usage: %s set <path>=<value>", slash)}
	}

	var path, rawValue string

	// Try path=value syntax first.
	eqIdx := -1
	for i, c := range args {
		if c == '=' {
			eqIdx = i
			break
		}
	}
	if eqIdx > 0 {
		path = args[:eqIdx]
		rawValue = args[eqIdx+1:]
	} else {
		// Try path <space> value syntax.
		for i, c := range args {
			if c == ' ' || c == '\t' {
				path = args[:i]
				rawValue = args[i+1:]
				break
			}
		}
	}

	if path == "" || rawValue == "" {
		return &ConfigCommand{Action: ConfigActionError, Message: fmt.Sprintf("Usage: %s set <path>=<value>", slash)}
	}

	result := ParseConfigValue(rawValue)
	if result.Error != "" {
		return &ConfigCommand{Action: ConfigActionError, Message: result.Error}
	}

	return &ConfigCommand{Action: ConfigActionSet, Path: path, Value: result.Value}
}

// --- Config write authorization ---

// ConfigWriteAuthResult holds the result of a config write authorization check.
type ConfigWriteAuthResult struct {
	Allowed bool
	Reason  string
}

// ConfigWriteScope describes the origin scope for a config write.
type ConfigWriteScope struct {
	ChannelID string
	AccountID string
}

// ConfigWriteTarget describes the target scope for a config write.
type ConfigWriteTarget string

const (
	ConfigWriteTargetGlobal  ConfigWriteTarget = "global"
	ConfigWriteTargetChannel ConfigWriteTarget = "channel"
	ConfigWriteTargetAccount ConfigWriteTarget = "account"
)

// AuthorizeConfigWrite checks if a config write is allowed for the given scope.
func AuthorizeConfigWrite(origin ConfigWriteScope, target ConfigWriteTarget, isAdmin bool) ConfigWriteAuthResult {
	if isAdmin {
		return ConfigWriteAuthResult{Allowed: true}
	}
	if target == ConfigWriteTargetGlobal {
		return ConfigWriteAuthResult{
			Allowed: false,
			Reason:  "Global config writes require operator.admin scope.",
		}
	}
	if origin.ChannelID == "" {
		return ConfigWriteAuthResult{
			Allowed: false,
			Reason:  "Config writes require a channel context.",
		}
	}
	return ConfigWriteAuthResult{Allowed: true}
}

// CanBypassConfigWritePolicy checks if a gateway client can bypass config
// write restrictions based on its scopes.
func CanBypassConfigWritePolicy(channel string, gatewayClientScopes []string) bool {
	for _, scope := range gatewayClientScopes {
		if scope == "operator.admin" || scope == "config.write" {
			return true
		}
	}
	return false
}
