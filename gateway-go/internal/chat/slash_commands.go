package chat

import (
	"fmt"
	"strings"
)

// SlashResult holds the result of parsing a slash command from user input.
type SlashResult struct {
	Handled  bool   // If true, the message was a slash command and should not be sent to LLM.
	Response string // Direct response to send back to the user.
	Command  string // The parsed command name (e.g., "reset", "model").
	Args     string // Arguments after the command.
}

// ParseSlashCommand checks if a message starts with a slash command.
// Returns nil if the message is not a slash command.
func ParseSlashCommand(text string) *SlashResult {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	// Extract command and args.
	parts := strings.SplitN(trimmed[1:], " ", 2)
	cmd := strings.ToLower(parts[0])

	// Strip Telegram bot username suffix (e.g., "/reset@MyBot" → "reset").
	if idx := strings.IndexByte(cmd, '@'); idx != -1 {
		cmd = cmd[:idx]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "reset":
		return &SlashResult{
			Handled:  true,
			Response: "세션이 초기화되었습니다.",
			Command:  "reset",
		}
	case "status":
		return &SlashResult{
			Handled:  true,
			Response: "", // Will be filled by the handler with actual status.
			Command:  "status",
		}
	case "kill", "stop", "cancel":
		return &SlashResult{
			Handled:  true,
			Response: "실행이 중단되었습니다.",
			Command:  "kill",
		}
	case "model":
		// Accepts model ID ("google/gemini-3.1-pro") or role name ("main", "lightweight", "pilot", "fallback").
		if args == "" {
			return &SlashResult{
				Handled:  true,
				Response: "사용법: /model <model-name 또는 역할명(main|lightweight|pilot|fallback)>",
				Command:  "model",
			}
		}
		if args == "" {
			return &SlashResult{
				Handled:  true,
				Response: "사용법: /model <model-name 또는 역할명(main|lightweight|fallback|image)>",
				Command:  "model",
			}
		}
		return &SlashResult{
			Handled:  true,
			Response: fmt.Sprintf("모델이 %q(으)로 변경되었습니다.", args),
			Command:  "model",
			Args:     args,
		}
	case "models":
		return &SlashResult{
			Handled:  true,
			Response: "모델 퀵체인지는 텔레그램에서만 지원됩니다.",
			Command:  "models",
		}
	case "think":
		return &SlashResult{
			Handled:  true,
			Response: "사고 모드가 토글되었습니다.",
			Command:  "think",
		}
	case "coordinator", "코디네이터":
		return &SlashResult{
			Handled:  true,
			Response: "코디네이터 모드가 활성화되었습니다. 워커 에이전트를 조율하여 작업을 수행합니다.",
			Command:  "coordinator",
		}
	case "chart":
		return &SlashResult{
			Handled:  true,
			Response: "",
			Command:  "chart",
		}
	default:
		// Not a recognized slash command; pass through to LLM.
		return nil
	}
}
