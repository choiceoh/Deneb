package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SlashCommand defines a bot command for Telegram's command menu.
type SlashCommand struct {
	// Command is the command name without the leading slash (e.g. "dashboard").
	Command string
	// Description is the Korean description shown in the Telegram command menu.
	Description string
	// Aliases are alternative command names that map to this command.
	Aliases []string
}

// BotCommand is the Telegram API type for setMyCommands.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// VibeCoderCommands returns the slash commands available for the vibe coder workflow.
// Only 4 commands exist — developer-focused commands (file, grep, run, etc.) were
// intentionally removed because the user is a vibe coder who does not read code.
func VibeCoderCommands() []SlashCommand {
	return []SlashCommand{
		{
			Command:     "dashboard",
			Description: "프로젝트 상태 대시보드",
			Aliases:     []string{"d", "status", "ws"},
		},
		{
			Command:     "commit",
			Description: "변경사항 커밋",
			Aliases:     nil,
		},
		{
			Command:     "push",
			Description: "원격 저장소에 푸시",
			Aliases:     nil,
		},
		{
			Command:     "chart",
			Description: "실험 차트 보기",
			Aliases:     nil,
		},
		{
			Command:     "models",
			Description: "모델 퀵체인지",
			Aliases:     nil,
		},
		{
			Command:     "help",
			Description: "도움말",
			Aliases:     nil,
		},
	}
}

// RegisterCommands sends the bot command list to Telegram via the setMyCommands API.
// This registers commands (including aliases) so they appear in the Telegram command menu
// when the user types "/". Only call this once during plugin startup.
func RegisterCommands(ctx context.Context, client *Client, commands []SlashCommand) error {
	botCommands := toBotCommands(commands)
	if len(botCommands) == 0 {
		return nil
	}

	params := map[string]any{
		"commands": botCommands,
	}

	_, err := client.Call(ctx, "setMyCommands", params)
	if err != nil {
		return fmt.Errorf("setMyCommands: %w", err)
	}
	return nil
}

// toBotCommands expands SlashCommands (with aliases) into a flat list of BotCommand
// entries suitable for the Telegram API. Each alias gets its own entry with the same
// description as the parent command.
func toBotCommands(commands []SlashCommand) []BotCommand {
	var result []BotCommand
	for _, cmd := range commands {
		result = append(result, BotCommand{
			Command:     cmd.Command,
			Description: cmd.Description,
		})
		for _, alias := range cmd.Aliases {
			result = append(result, BotCommand{
				Command:     alias,
				Description: cmd.Description,
			})
		}
	}
	return result
}

// IsSlashCommand checks if a message starts with a known slash command.
// Returns the command name (without slash), remaining arguments, and whether a match was found.
// The command check is case-insensitive.
func IsSlashCommand(text string) (command, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	// Strip the leading slash and split into command + args.
	withoutSlash := text[1:]

	// Handle bot-suffixed commands like "/dashboard@mybot_bot".
	var rest string
	spaceIdx := strings.IndexByte(withoutSlash, ' ')
	if spaceIdx >= 0 {
		rest = strings.TrimSpace(withoutSlash[spaceIdx+1:])
		withoutSlash = withoutSlash[:spaceIdx]
	}

	// Strip @bot suffix if present (Telegram appends @botname in groups).
	if atIdx := strings.IndexByte(withoutSlash, '@'); atIdx >= 0 {
		withoutSlash = withoutSlash[:atIdx]
	}

	cmd := strings.ToLower(withoutSlash)
	if cmd == "" {
		return "", "", false
	}

	// Check against known commands and aliases.
	commands := VibeCoderCommands()
	for _, sc := range commands {
		if cmd == sc.Command {
			return sc.Command, rest, true
		}
		for _, alias := range sc.Aliases {
			if cmd == alias {
				return sc.Command, rest, true
			}
		}
	}

	return "", "", false
}

// IsCommandAlias checks if the text matches any command alias and returns the
// canonical command name. This is useful for resolving aliases before dispatch.
func IsCommandAlias(text string, commands []SlashCommand) (canonical string, ok bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		text = text[1:]
	}

	// Strip @bot suffix.
	if atIdx := strings.IndexByte(text, '@'); atIdx >= 0 {
		text = text[:atIdx]
	}

	// Take only the command part (before any space).
	if spaceIdx := strings.IndexByte(text, ' '); spaceIdx >= 0 {
		text = text[:spaceIdx]
	}

	cmd := strings.ToLower(text)
	if cmd == "" {
		return "", false
	}

	for _, sc := range commands {
		if cmd == sc.Command {
			return sc.Command, true
		}
		for _, alias := range sc.Aliases {
			if cmd == alias {
				return sc.Command, true
			}
		}
	}
	return "", false
}

// ClearCommands removes all registered bot commands from Telegram.
// Useful during shutdown or when switching command sets.
func ClearCommands(ctx context.Context, client *Client) error {
	_, err := client.Call(ctx, "deleteMyCommands", nil)
	if err != nil {
		return fmt.Errorf("deleteMyCommands: %w", err)
	}
	return nil
}

// GetRegisteredCommands fetches the currently registered bot commands from Telegram.
func GetRegisteredCommands(ctx context.Context, client *Client) ([]BotCommand, error) {
	result, err := client.CallIdempotent(ctx, "getMyCommands", nil)
	if err != nil {
		return nil, fmt.Errorf("getMyCommands: %w", err)
	}

	var commands []BotCommand
	if err := json.Unmarshal(result, &commands); err != nil {
		return nil, fmt.Errorf("decode getMyCommands: %w", err)
	}
	return commands, nil
}
