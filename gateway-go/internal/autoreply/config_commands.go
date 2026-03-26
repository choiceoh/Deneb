// config_commands.go — Config command parsing and value coercion.
// Mirrors src/auto-reply/reply/config-commands.ts (23 LOC),
// config-value.ts (49 LOC), config-write-authorization.ts (34 LOC),
// commands-setunset.ts (102 LOC), commands-setunset-standard.ts (24 LOC),
// commands-slash-parse.ts (44 LOC).
//
// Implements the full slash command parsing pipeline:
// parseSlashCommandOrNull → parseSlashCommandWithSetUnset → parseConfigCommand
package autoreply

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// --- Slash command parsing (mirrors commands-slash-parse.ts) ---

// SlashParseResult holds the result of parsing a slash command.
type SlashParseResult struct {
	OK      bool
	Action  string
	Args    string
	Message string // error message when !OK
}

// ParseSlashCommandOrNull extracts action and args from a slash command.
// Returns nil if the text doesn't start with the expected slash prefix.
func ParseSlashCommandOrNull(raw string, slash string, defaultAction string, invalidMessage string) *SlashParseResult {
	trimmed := strings.TrimSpace(raw)
	slashLower := strings.ToLower(slash)
	if !strings.HasPrefix(strings.ToLower(trimmed), slashLower) {
		return nil
	}
	rest := strings.TrimSpace(trimmed[len(slash):])
	// Handle colon syntax: /config:set → action=set
	if strings.HasPrefix(rest, ":") {
		rest = strings.TrimSpace(rest[1:])
	}
	if rest == "" {
		if defaultAction == "" {
			defaultAction = "show"
		}
		return &SlashParseResult{OK: true, Action: defaultAction}
	}

	// Split into action + args.
	fields := strings.SplitN(rest, " ", 2)
	action := strings.ToLower(strings.TrimSpace(fields[0]))
	args := ""
	if len(fields) > 1 {
		args = strings.TrimSpace(fields[1])
	}
	if action == "" {
		return &SlashParseResult{OK: false, Message: invalidMessage}
	}
	return &SlashParseResult{OK: true, Action: action, Args: args}
}

// --- Config command types ---

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
//
// Implements the TS pipeline: parseStandardSetUnsetSlashCommand → onKnownAction
// → set/unset handlers with parseConfigValue coercion.
func ParseConfigCommand(raw string) *ConfigCommand {
	parsed := ParseSlashCommandOrNull(raw, "/config", "show", "Invalid /config syntax.")
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
	if eqIdx := strings.Index(args, "="); eqIdx > 0 {
		path = strings.TrimSpace(args[:eqIdx])
		rawValue = args[eqIdx+1:]
	} else {
		// Try path <space> value syntax.
		fields := strings.SplitN(args, " ", 2)
		if len(fields) < 2 {
			return &ConfigCommand{Action: ConfigActionError, Message: fmt.Sprintf("Usage: %s set <path>=<value>", slash)}
		}
		path = strings.TrimSpace(fields[0])
		rawValue = strings.TrimSpace(fields[1])
	}

	if path == "" {
		return &ConfigCommand{Action: ConfigActionError, Message: fmt.Sprintf("Usage: %s set <path>=<value>", slash)}
	}

	value, errMsg := ParseConfigValue(rawValue)
	if errMsg != "" {
		return &ConfigCommand{Action: ConfigActionError, Message: errMsg}
	}

	return &ConfigCommand{Action: ConfigActionSet, Path: path, Value: value}
}

// --- Config value parsing (mirrors config-value.ts) ---

// ParseConfigValue coerces a user-provided config value string into a typed value.
// Returns the parsed value and an error message (empty if no error).
//
// Coercion order (matching TS): JSON object/array → boolean → null → number → quoted string → plain string.
func ParseConfigValue(raw string) (value any, errMsg string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, "Missing value."
	}

	// JSON object or array.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, fmt.Sprintf("Invalid JSON: %s", err.Error())
		}
		return parsed, ""
	}

	// Boolean literals.
	if trimmed == "true" {
		return true, ""
	}
	if trimmed == "false" {
		return false, ""
	}

	// Null.
	if trimmed == "null" {
		return nil, ""
	}

	// Number (integer or float).
	if num, err := strconv.ParseFloat(trimmed, 64); err == nil {
		if !isInfiniteOrNaN(num) {
			return num, ""
		}
	}

	// Quoted string (double or single).
	if (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) ||
		(strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`)) {
		// Try JSON parse for proper escape handling.
		var parsed string
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed, ""
		}
		// Fallback: strip quotes.
		return trimmed[1 : len(trimmed)-1], ""
	}

	// Plain string.
	return trimmed, ""
}

func isInfiniteOrNaN(f float64) bool {
	return f != f || f > 1.7976931348623157e+308 || f < -1.7976931348623157e+308
}

// --- Debug command parsing ---

// DebugCommand represents a parsed /debug command.
type DebugCommand struct {
	Action  ConfigCommandAction
	Path    string
	Value   any
	Message string
}

// ParseDebugCommand parses a /debug command from raw text.
func ParseDebugCommand(raw string) *DebugCommand {
	parsed := ParseSlashCommandOrNull(raw, "/debug", "show", "Invalid /debug syntax.")
	if parsed == nil {
		return nil
	}
	if !parsed.OK {
		return &DebugCommand{Action: ConfigActionError, Message: parsed.Message}
	}

	switch parsed.Action {
	case "show", "get":
		return &DebugCommand{Action: ConfigActionShow, Path: parsed.Args}

	case "unset", "reset":
		if parsed.Args == "" {
			// /debug reset with no path resets all overrides.
			return &DebugCommand{Action: ConfigActionUnset}
		}
		return &DebugCommand{Action: ConfigActionUnset, Path: parsed.Args}

	case "set":
		cmd := parseConfigSetCommand("/debug", parsed.Args)
		return &DebugCommand{Action: cmd.Action, Path: cmd.Path, Value: cmd.Value, Message: cmd.Message}

	default:
		return &DebugCommand{Action: ConfigActionError, Message: "Usage: /debug show|set|unset|reset"}
	}
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
