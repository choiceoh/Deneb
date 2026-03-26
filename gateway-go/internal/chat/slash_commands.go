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
		if args == "" {
			return &SlashResult{
				Handled:  true,
				Response: "사용법: /model <model-name>",
				Command:  "model",
			}
		}
		return &SlashResult{
			Handled:  true,
			Response: fmt.Sprintf("모델이 %q(으)로 변경되었습니다.", args),
			Command:  "model",
			Args:     args,
		}
	case "think":
		return &SlashResult{
			Handled:  true,
			Response: "사고 모드가 토글되었습니다.",
			Command:  "think",
		}
	default:
		// Not a recognized slash command; pass through to LLM.
		return nil
	}
}
