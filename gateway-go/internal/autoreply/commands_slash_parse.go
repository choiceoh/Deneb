// commands_slash_parse.go — Slash command action+args parser.
// Mirrors src/auto-reply/reply/commands-slash-parse.ts (46 LOC).
package autoreply

import (
	"regexp"
	"strings"
)

// SlashParseKind categorizes a slash command parse attempt.
type SlashParseKind int

const (
	SlashNoMatch SlashParseKind = iota
	SlashEmpty
	SlashInvalid
	SlashParsed
)

// SlashCommandParseResult holds the raw parse outcome.
type SlashCommandParseResult struct {
	Kind   SlashParseKind
	Action string
	Args   string
}

// ParsedSlashCommand is the high-level parse result (ok or error).
type ParsedSlashCommand struct {
	OK      bool
	Action  string
	Args    string
	Message string // set when OK == false
}

var slashActionRe = regexp.MustCompile(`^(\S+)(?:\s+([\s\S]+))?$`)

// ParseSlashCommandActionArgs extracts action and args from a slash command.
func ParseSlashCommandActionArgs(raw, slash string) SlashCommandParseResult {
	trimmed := strings.TrimSpace(raw)
	slashLower := strings.ToLower(slash)
	if !strings.HasPrefix(strings.ToLower(trimmed), slashLower) {
		return SlashCommandParseResult{Kind: SlashNoMatch}
	}
	rest := strings.TrimSpace(trimmed[len(slash):])
	if rest == "" {
		return SlashCommandParseResult{Kind: SlashEmpty}
	}
	m := slashActionRe.FindStringSubmatch(rest)
	if m == nil {
		return SlashCommandParseResult{Kind: SlashInvalid}
	}
	action := strings.ToLower(m[1])
	args := ""
	if len(m) > 2 {
		args = strings.TrimSpace(m[2])
	}
	return SlashCommandParseResult{Kind: SlashParsed, Action: action, Args: args}
}

// ParseSlashCommandOrNull returns a high-level parsed result, or nil if the
// command doesn't match the given slash prefix.
func ParseSlashCommandOrNull(raw, slash, invalidMessage, defaultAction string) *ParsedSlashCommand {
	parsed := ParseSlashCommandActionArgs(raw, slash)
	switch parsed.Kind {
	case SlashNoMatch:
		return nil
	case SlashInvalid:
		return &ParsedSlashCommand{OK: false, Message: invalidMessage}
	case SlashEmpty:
		action := defaultAction
		if action == "" {
			action = "show"
		}
		return &ParsedSlashCommand{OK: true, Action: action}
	default:
		return &ParsedSlashCommand{OK: true, Action: parsed.Action, Args: parsed.Args}
	}
}
