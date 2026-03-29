// Package discord — Application Command (slash command) registration.
//
// Registers slash commands with the Discord API so they appear in the
// autocomplete picker when users type "/" in a channel. This is essential
// for vibe-coding workflows where the user may not know all available commands.
package discord

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// ApplicationCommand represents a Discord Application Command for registration.
type ApplicationCommand struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Type        int                        `json:"type,omitempty"`        // 1=CHAT_INPUT (default)
	Options     []ApplicationCommandOption `json:"options,omitempty"`
}

// ApplicationCommandOption represents an option for a slash command.
type ApplicationCommandOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        int    `json:"type"`               // 3=STRING, 4=INTEGER, 5=BOOLEAN
	Required    bool   `json:"required,omitempty"`
}

// Option type constants.
const (
	OptionTypeString  = 3
	OptionTypeInteger = 4
	OptionTypeBoolean = 5
)

// CodingCommands returns Discord slash commands for the vibe-coding channel.
// Only includes commands that make sense for a non-developer who works entirely
// through natural language instructions to the AI agent.
func CodingCommands() []ApplicationCommand {
	return []ApplicationCommand{
		{Name: "dashboard", Description: "프로젝트 현황 한눈에 보기 (빌드·테스트·브랜치)"},
		{Name: "commit", Description: "변경 사항 저장", Options: []ApplicationCommandOption{
			{Name: "message", Description: "커밋 메시지 (비워두면 자동 생성)", Type: OptionTypeString},
		}},
		{Name: "push", Description: "원격 저장소에 업로드"},
		{Name: "help", Description: "사용 가능한 명령어 보기"},
	}
}

// RegisterCommands registers all coding slash commands with the Discord API.
// If guildID is provided, registers as guild commands (instant); otherwise global
// commands (may take up to 1 hour to propagate).
func RegisterCommands(ctx context.Context, client *Client, appID, guildID string, logger *slog.Logger) error {
	commands := CodingCommands()

	var path string
	if guildID != "" {
		path = fmt.Sprintf("/applications/%s/guilds/%s/commands", appID, guildID)
	} else {
		path = fmt.Sprintf("/applications/%s/commands", appID)
	}

	// Bulk overwrite: PUT replaces all commands at once.
	_, err := client.Call(ctx, http.MethodPut, path, commands)
	if err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}

	logger.Info("discord: registered slash commands",
		"count", len(commands),
		"scope", scopeLabel(guildID))

	return nil
}

func scopeLabel(guildID string) string {
	if guildID != "" {
		return "guild"
	}
	return "global"
}
