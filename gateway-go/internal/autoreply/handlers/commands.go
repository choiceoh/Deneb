package handlers

import (
	"regexp"
	"strings"
	"sync"
)

// CommandScope controls where a command is available.
type CommandScope string

const (
	ScopeText   CommandScope = "text"
	ScopeNative CommandScope = "native"
	ScopeBoth   CommandScope = "both"
)

// CommandCategory groups commands for help/status display.
type CommandCategory string

const (
	CategorySession    CommandCategory = "session"
	CategoryOptions    CommandCategory = "options"
	CategoryStatus     CommandCategory = "status"
	CategoryManagement CommandCategory = "management"
	CategoryMedia      CommandCategory = "media"
	CategoryTools      CommandCategory = "tools"
	CategoryDocks      CommandCategory = "docks"
)

// CommandArgDefinition describes a command argument.
type CommandArgDefinition struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Type             string   `json:"type"` // "string", "number", "boolean"
	Required         bool     `json:"required,omitempty"`
	Choices          []string `json:"choices,omitempty"`
	CaptureRemaining bool     `json:"captureRemaining,omitempty"`
}

// ChatCommandDefinition defines a chat command with aliases and metadata.
type ChatCommandDefinition struct {
	Key         string                 `json:"key"`
	NativeName  string                 `json:"nativeName,omitempty"`
	Description string                 `json:"description"`
	TextAliases []string               `json:"textAliases"`
	AcceptsArgs bool                   `json:"acceptsArgs,omitempty"`
	Args        []CommandArgDefinition `json:"args,omitempty"`
	ArgsParsing string                 `json:"argsParsing,omitempty"` // "none" or "positional"
	Scope       CommandScope           `json:"scope"`
	Category    CommandCategory        `json:"category,omitempty"`
}

// NativeCommandSpec describes a command for native platform registration (Discord).
type NativeCommandSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	AcceptsArgs bool                   `json:"acceptsArgs"`
	Args        []CommandArgDefinition `json:"args,omitempty"`
}

// CommandArgs holds parsed command arguments.
type CommandArgs struct {
	Raw    string            `json:"raw,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

// CommandDetection holds precomputed structures for fast command detection.
type CommandDetection struct {
	Exact map[string]bool
	Regex *regexp.Regexp
}

// textAliasSpec maps a normalized alias to its canonical form.
type textAliasSpec struct {
	key         string
	canonical   string
	acceptsArgs bool
}

// CommandRegistry manages command definitions and provides detection/normalization.
type CommandRegistry struct {
	mu        sync.RWMutex
	commands  []ChatCommandDefinition
	aliasMap  map[string]*textAliasSpec
	detection *CommandDetection
}

// NewCommandRegistry creates a new registry with the given command definitions.
func NewCommandRegistry(commands []ChatCommandDefinition) *CommandRegistry {
	r := &CommandRegistry{
		commands: commands,
	}
	r.rebuild()
	return r
}

// SetCommands replaces the command list and invalidates caches.
func (r *CommandRegistry) SetCommands(commands []ChatCommandDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = commands
	r.aliasMap = nil
	r.detection = nil
	r.rebuild()
}

// Commands returns the current command list.
func (r *CommandRegistry) Commands() []ChatCommandDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ChatCommandDefinition, len(r.commands))
	copy(result, r.commands)
	return result
}

func (r *CommandRegistry) rebuild() {
	// Build alias map.
	aliasMap := make(map[string]*textAliasSpec, len(r.commands)*3)
	for _, cmd := range r.commands {
		canonical := cmd.Key
		if len(cmd.TextAliases) > 0 {
			c := strings.TrimSpace(cmd.TextAliases[0])
			if c != "" {
				canonical = c
			} else {
				canonical = "/" + cmd.Key
			}
		} else {
			canonical = "/" + cmd.Key
		}
		for _, alias := range cmd.TextAliases {
			normalized := strings.ToLower(strings.TrimSpace(alias))
			if normalized == "" {
				continue
			}
			if _, exists := aliasMap[normalized]; !exists {
				aliasMap[normalized] = &textAliasSpec{
					key:         cmd.Key,
					canonical:   canonical,
					acceptsArgs: cmd.AcceptsArgs,
				}
			}
		}
	}
	r.aliasMap = aliasMap

	// Build detection.
	exact := make(map[string]bool, len(aliasMap))
	var patterns []string
	for _, cmd := range r.commands {
		for _, alias := range cmd.TextAliases {
			normalized := strings.ToLower(strings.TrimSpace(alias))
			if normalized == "" {
				continue
			}
			exact[normalized] = true
			escaped := regexp.QuoteMeta(normalized)
			if cmd.AcceptsArgs {
				patterns = append(patterns, escaped+`(?:\s+.+|\s*:\s*.*)?`)
			} else {
				patterns = append(patterns, escaped+`(?:\s*:\s*)?`)
			}
		}
	}

	var re *regexp.Regexp
	if len(patterns) > 0 {
		re = regexp.MustCompile(`(?i)^(?:` + strings.Join(patterns, "|") + `)$`)
	} else {
		re = regexp.MustCompile(`$^`) // never matches
	}
	r.detection = &CommandDetection{Exact: exact, Regex: re}
}

// NormalizeCommandBody normalizes a command string: strips bot mentions,
// handles colon syntax, and resolves text aliases.
func (r *CommandRegistry) NormalizeCommandBody(raw string, botUsername string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") {
		return trimmed
	}

	// Extract first line.
	singleLine := trimmed
	if idx := strings.IndexByte(trimmed, '\n'); idx != -1 {
		singleLine = strings.TrimSpace(trimmed[:idx])
	}

	// Handle colon syntax: /command:args → /command args
	if m := colonCmdRe.FindStringSubmatch(singleLine); m != nil {
		rest := strings.TrimLeft(m[2], " \t")
		if rest != "" {
			singleLine = "/" + m[1] + " " + rest
		} else {
			singleLine = "/" + m[1]
		}
	}

	// Strip @bot mention from command.
	if botUsername != "" {
		normalizedBot := strings.ToLower(strings.TrimSpace(botUsername))
		if m := mentionCmdRe.FindStringSubmatch(singleLine); m != nil {
			if strings.ToLower(m[2]) == normalizedBot {
				rest := ""
				if len(m) > 3 {
					rest = m[3]
				}
				singleLine = "/" + m[1] + rest
			}
		}
	}

	r.mu.RLock()
	aliasMap := r.aliasMap
	r.mu.RUnlock()

	// Exact alias match.
	lowered := strings.ToLower(singleLine)
	if spec, ok := aliasMap[lowered]; ok {
		return spec.canonical
	}

	// Token-based alias match (command with args).
	tokenMatch := tokenCmdRe.FindStringSubmatch(singleLine)
	if tokenMatch == nil {
		return singleLine
	}
	token := tokenMatch[1]
	rest := ""
	if len(tokenMatch) > 2 {
		rest = tokenMatch[2]
	}
	tokenKey := "/" + strings.ToLower(token)
	spec, ok := aliasMap[tokenKey]
	if !ok {
		return singleLine
	}
	if rest != "" && !spec.acceptsArgs {
		return singleLine
	}
	normalizedRest := strings.TrimLeft(rest, " \t")
	if normalizedRest != "" {
		return spec.canonical + " " + normalizedRest
	}
	return spec.canonical
}

// GetDetection returns the precomputed command detection data.
func (r *CommandRegistry) GetDetection() *CommandDetection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.detection
}

// HasControlCommand returns true if the text contains a recognized command.
func (r *CommandRegistry) HasControlCommand(text, botUsername string) bool {
	if text == "" {
		return false
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	normalizedBody := r.NormalizeCommandBody(trimmed, botUsername)
	if normalizedBody == "" {
		return false
	}
	lowered := strings.ToLower(normalizedBody)

	r.mu.RLock()
	commands := r.commands
	r.mu.RUnlock()

	for _, cmd := range commands {
		for _, alias := range cmd.TextAliases {
			normalized := strings.ToLower(strings.TrimSpace(alias))
			if normalized == "" {
				continue
			}
			if lowered == normalized {
				return true
			}
			if cmd.AcceptsArgs && strings.HasPrefix(lowered, normalized) {
				nextIdx := len(normalized)
				if nextIdx < len(normalizedBody) {
					ch := rune(normalizedBody[nextIdx])
					if ch == ' ' || ch == '\t' || ch == '\n' {
						return true
					}
				}
			}
		}
	}
	return false
}

// MaybeResolveTextAlias returns the canonical command key if the text
// matches a known command alias, or "" if not.
func (r *CommandRegistry) MaybeResolveTextAlias(raw string) string {
	trimmed := strings.TrimSpace(r.NormalizeCommandBody(raw, ""))
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	r.mu.RLock()
	det := r.detection
	aliasMap := r.aliasMap
	r.mu.RUnlock()

	normalized := strings.ToLower(trimmed)
	if det.Exact[normalized] {
		return normalized
	}
	if !det.Regex.MatchString(normalized) {
		return ""
	}
	tokenMatch := tokenResolveRe.FindStringSubmatch(normalized)
	if tokenMatch == nil {
		return ""
	}
	tokenKey := "/" + tokenMatch[1]
	if _, ok := aliasMap[tokenKey]; ok {
		return tokenKey
	}
	return ""
}

// HasInlineCommandTokens returns true if text contains inline /cmd or !cmd tokens.
func HasInlineCommandTokens(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return inlineCmdRe.MatchString(text)
}

// ParseCommandArgs parses raw argument text for a command definition.
func ParseCommandArgs(cmd *ChatCommandDefinition, raw string) *CommandArgs {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if len(cmd.Args) == 0 || cmd.ArgsParsing == "none" {
		return &CommandArgs{Raw: trimmed}
	}
	values := parsePositionalArgs(cmd.Args, trimmed)
	return &CommandArgs{Raw: trimmed, Values: values}
}

func parsePositionalArgs(defs []CommandArgDefinition, raw string) map[string]string {
	values := make(map[string]string)
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return values
	}
	tokens := strings.Fields(trimmed)
	idx := 0
	for _, def := range defs {
		if idx >= len(tokens) {
			break
		}
		if def.CaptureRemaining {
			values[def.Name] = strings.Join(tokens[idx:], " ")
			break
		}
		values[def.Name] = tokens[idx]
		idx++
	}
	return values
}

// BuildCommandText constructs a slash command string.
func BuildCommandText(commandName, args string) string {
	trimmedArgs := strings.TrimSpace(args)
	if trimmedArgs != "" {
		return "/" + commandName + " " + trimmedArgs
	}
	return "/" + commandName
}

// ListNativeCommandSpecs returns specs for native platform command registration.
func (r *CommandRegistry) ListNativeCommandSpecs() []NativeCommandSpec {
	r.mu.RLock()
	commands := r.commands
	r.mu.RUnlock()

	var specs []NativeCommandSpec
	for _, cmd := range commands {
		if cmd.Scope == ScopeText || cmd.NativeName == "" {
			continue
		}
		name := resolveNativeName(cmd)
		specs = append(specs, NativeCommandSpec{
			Name:        name,
			Description: cmd.Description,
			AcceptsArgs: cmd.AcceptsArgs,
			Args:        cmd.Args,
		})
	}
	return specs
}

// FindCommandByNativeName finds a command by its native platform name.
func (r *CommandRegistry) FindCommandByNativeName(name string) *ChatCommandDefinition {
	normalized := strings.ToLower(strings.TrimSpace(name))
	r.mu.RLock()
	commands := r.commands
	r.mu.RUnlock()

	for _, cmd := range commands {
		if cmd.Scope == ScopeText {
			continue
		}
		if strings.ToLower(resolveNativeName(cmd)) == normalized {
			c := cmd // copy
			return &c
		}
	}
	return nil
}

func resolveNativeName(cmd ChatCommandDefinition) string {
	if cmd.NativeName == "" {
		return cmd.Key
	}
	return cmd.NativeName
}

// Precompiled regexes for command normalization.
var (
	colonCmdRe     = regexp.MustCompile(`^/([^\s:]+)\s*:(.*)$`)
	mentionCmdRe   = regexp.MustCompile(`^/([^\s@]+)@([^\s]+)(.*)$`)
	tokenCmdRe     = regexp.MustCompile(`^/([^\s]+)(?:\s+([\s\S]+))?$`)
	tokenResolveRe = regexp.MustCompile(`^/([^\s:]+)(?:\s|$)`)
	inlineCmdRe    = regexp.MustCompile(`(?:^|\s)[/!][a-zA-Z]`)
)
