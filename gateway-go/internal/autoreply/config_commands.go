// config_commands.go — Config command parsing and value coercion.
// Mirrors src/auto-reply/reply/config-commands.ts (23 LOC),
// config-value.ts (49 LOC), config-write-authorization.ts (34 LOC).
package autoreply

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

var configCommandRe = regexp.MustCompile(`(?i)^/config(?:\s+|:\s*)(.*)$`)

// ParseConfigCommand parses a /config command from raw text.
// Returns nil if the text is not a /config command.
func ParseConfigCommand(raw string) *ConfigCommand {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), "/config") {
		return nil
	}

	m := configCommandRe.FindStringSubmatch(trimmed)
	var args string
	if m != nil {
		args = strings.TrimSpace(m[1])
	}

	if args == "" {
		return &ConfigCommand{Action: ConfigActionShow}
	}

	// Parse action.
	fields := strings.SplitN(args, " ", 2)
	action := strings.ToLower(fields[0])
	rest := ""
	if len(fields) > 1 {
		rest = strings.TrimSpace(fields[1])
	}

	switch action {
	case "show", "get":
		return &ConfigCommand{Action: ConfigActionShow, Path: rest}

	case "unset":
		if rest == "" {
			return &ConfigCommand{
				Action:  ConfigActionError,
				Message: "Usage: /config unset <path>",
			}
		}
		return &ConfigCommand{Action: ConfigActionUnset, Path: rest}

	case "set":
		return parseConfigSetArgs(rest)

	default:
		// Treat as show with path.
		return &ConfigCommand{Action: ConfigActionShow, Path: args}
	}
}

func parseConfigSetArgs(rest string) *ConfigCommand {
	if rest == "" {
		return &ConfigCommand{
			Action:  ConfigActionError,
			Message: "Usage: /config set <path> <value>",
		}
	}

	// Split path and value on first space or '='.
	var path, rawValue string
	if idx := strings.IndexAny(rest, " ="); idx >= 0 {
		path = rest[:idx]
		rawValue = strings.TrimSpace(rest[idx+1:])
	} else {
		return &ConfigCommand{
			Action:  ConfigActionError,
			Message: "Usage: /config set <path> <value>",
		}
	}

	if path == "" || rawValue == "" {
		return &ConfigCommand{
			Action:  ConfigActionError,
			Message: "Usage: /config set <path> <value>",
		}
	}

	value, err := ParseConfigValue(rawValue)
	if err != "" {
		return &ConfigCommand{Action: ConfigActionError, Message: err}
	}

	return &ConfigCommand{Action: ConfigActionSet, Path: path, Value: value}
}

// ParseConfigValue coerces a user-provided config value string into a typed value.
// Returns the parsed value and an error message (empty if no error).
func ParseConfigValue(raw string) (value any, errMsg string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, "Missing value."
	}

	// Try JSON object/array.
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

	// Number.
	if num, err := strconv.ParseFloat(trimmed, 64); err == nil {
		if !isInfiniteOrNaN(num) {
			return num, ""
		}
	}

	// Quoted string.
	if (strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`)) ||
		(strings.HasPrefix(trimmed, `'`) && strings.HasSuffix(trimmed, `'`)) {
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
// This is a simplified version; the full implementation lives in the config subsystem.
func AuthorizeConfigWrite(origin ConfigWriteScope, target ConfigWriteTarget, isAdmin bool) ConfigWriteAuthResult {
	// Admins can always write.
	if isAdmin {
		return ConfigWriteAuthResult{Allowed: true}
	}

	// Non-admin writes to global config are denied.
	if target == ConfigWriteTargetGlobal {
		return ConfigWriteAuthResult{
			Allowed: false,
			Reason:  "Global config writes require operator.admin scope.",
		}
	}

	// Channel/account scoped writes are allowed for the owning channel.
	if origin.ChannelID == "" {
		return ConfigWriteAuthResult{
			Allowed: false,
			Reason:  "Config writes require a channel context.",
		}
	}

	return ConfigWriteAuthResult{Allowed: true}
}
