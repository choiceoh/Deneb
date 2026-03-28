// commands_slash_parse.go — Backward-compatible facade for slash command parsing.
package handlers

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/cmddispatch"

// SlashParseKind categorizes a slash command parse attempt.
type SlashParseKind = cmddispatch.SlashParseKind

const (
	SlashNoMatch = cmddispatch.SlashNoMatch
	SlashEmpty   = cmddispatch.SlashEmpty
	SlashInvalid = cmddispatch.SlashInvalid
	SlashParsed  = cmddispatch.SlashParsed
)

// SlashCommandParseResult holds the raw parse outcome.
type SlashCommandParseResult = cmddispatch.SlashCommandParseResult

// ParsedSlashCommand is the high-level parse result (ok or error).
type ParsedSlashCommand = cmddispatch.ParsedSlashCommand

// ParseSlashCommandActionArgs extracts action and args from a slash command.
func ParseSlashCommandActionArgs(raw, slash string) SlashCommandParseResult {
	return cmddispatch.ParseSlashCommandActionArgs(raw, slash)
}

// ParseSlashCommandOrNull returns a high-level parsed result, or nil if the
// command doesn't match the given slash prefix.
func ParseSlashCommandOrNull(raw, slash, invalidMessage, defaultAction string) *ParsedSlashCommand {
	return cmddispatch.ParseSlashCommandOrNull(raw, slash, invalidMessage, defaultAction)
}
