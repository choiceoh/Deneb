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

// CodingCommands returns all Discord slash commands for the coding channel.
func CodingCommands() []ApplicationCommand {
	return []ApplicationCommand{
		// File operations
		{Name: "file", Description: "파일 내용 보기 (구문 강조)", Options: []ApplicationCommandOption{
			{Name: "path", Description: "파일 경로", Type: OptionTypeString, Required: true},
			{Name: "start", Description: "시작 줄 번호", Type: OptionTypeInteger},
			{Name: "end", Description: "끝 줄 번호", Type: OptionTypeInteger},
		}},
		{Name: "grep", Description: "코드베이스에서 패턴 검색 (ripgrep)", Options: []ApplicationCommandOption{
			{Name: "pattern", Description: "검색 패턴 (정규식)", Type: OptionTypeString, Required: true},
			{Name: "glob", Description: "파일 필터 (예: *.go, *.ts)", Type: OptionTypeString},
		}},
		{Name: "tree", Description: "디렉토리 구조 보기", Options: []ApplicationCommandOption{
			{Name: "depth", Description: "탐색 깊이 (기본: 2)", Type: OptionTypeInteger},
		}},

		// Git operations
		{Name: "diff", Description: "Git 변경 통계 보기"},
		{Name: "gdiff", Description: "전체 Git diff 보기"},
		{Name: "log", Description: "최근 커밋 로그 보기", Options: []ApplicationCommandOption{
			{Name: "count", Description: "표시할 커밋 수 (기본: 10)", Type: OptionTypeInteger},
		}},
		{Name: "branch", Description: "Git 브랜치 목록 보기"},
		{Name: "blame", Description: "파일의 Git blame 조회", Options: []ApplicationCommandOption{
			{Name: "file", Description: "파일 경로", Type: OptionTypeString, Required: true},
			{Name: "line", Description: "줄 번호 (또는 시작 줄)", Type: OptionTypeInteger},
			{Name: "end_line", Description: "끝 줄 번호 (범위 지정 시)", Type: OptionTypeInteger},
		}},
		{Name: "stash", Description: "Git stash 관리", Options: []ApplicationCommandOption{
			{Name: "action", Description: "push, pop, list, show, drop", Type: OptionTypeString},
		}},
		{Name: "checkout", Description: "Git 브랜치 전환", Options: []ApplicationCommandOption{
			{Name: "branch", Description: "전환할 브랜치 이름", Type: OptionTypeString, Required: true},
		}},

		// Build & test
		{Name: "build", Description: "프로젝트 빌드 실행"},
		{Name: "test", Description: "프로젝트 테스트 실행"},
		{Name: "lint", Description: "코드 린트 검사"},
		{Name: "run", Description: "셸 명령어 직접 실행", Options: []ApplicationCommandOption{
			{Name: "command", Description: "실행할 명령어", Type: OptionTypeString, Required: true},
		}},

		// Deploy
		{Name: "commit", Description: "변경 사항 커밋", Options: []ApplicationCommandOption{
			{Name: "message", Description: "커밋 메시지 (비워두면 자동 생성)", Type: OptionTypeString},
		}},
		{Name: "push", Description: "현재 브랜치 원격 푸시"},

		// Status & session
		{Name: "ws", Description: "워크스페이스 상태 (브랜치, 변경사항, 최근 커밋)"},
		{Name: "agents", Description: "활성 에이전트 세션 목록 및 관리"},
		{Name: "help", Description: "사용 가능한 코딩 명령어 목록"},
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
