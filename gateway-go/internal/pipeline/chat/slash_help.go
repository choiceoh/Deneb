package chat

import "strings"

// slashHelpEntry is one row in the /help listing. usage is the command with its
// argument shape; desc is a Korean one-liner. This table is the single source
// for discovery — keep it in sync when adding a builtin slash command.
type slashHelpEntry struct {
	usage string
	desc  string
}

// slashBuiltinHelp lists the builtin slash commands. The layer is scoped to
// operational/session commands only — the conversational user commands
// (/model, /think, /pin, /mode, /mail, /insights, plus the Telegram-era
// /models and /use-forum) were removed in favor of the native client UI
// (model picker via miniapp.models.*) and config defaults.
var slashBuiltinHelp = []slashHelpEntry{
	{"/help", "이 도움말 (`/도움말`, `/?`)"},
	{"/status", "세션 상태·토큰·캐시 히트율"},
	{"/reset", "세션 초기화 (대화 기록 삭제)"},
	{"/kill", "실행 중단 (`/stop`, `/cancel`)"},
	{"/rollback", "변경 롤백 (`/롤백`)"},
	{"/update [확인]", "풀·빌드·재시작 (`/업데이트`)"},
	{"/restart", "게이트웨이 재시작 (`/재시작`)"},
}

// slashHelpText renders the builtin command list as Markdown for /help.
func slashHelpText() string {
	var b strings.Builder
	b.WriteString("🔧 **사용 가능한 명령어**\n\n")
	for _, e := range slashBuiltinHelp {
		b.WriteString("`")
		b.WriteString(e.usage)
		b.WriteString("` — ")
		b.WriteString(e.desc)
		b.WriteString("\n")
	}
	b.WriteString("\n설치된 스킬에 따라 스킬 명령어가 추가됩니다.")
	return b.String()
}
