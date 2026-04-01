package coordinator

import (
	"regexp"
	"strings"
)

// filePathPattern matches common source file paths (e.g., "src/main.go", "internal/chat/handler.go").
var filePathPattern = regexp.MustCompile(`(?:^|\s)[\w./\-]+\.\w{1,5}(?:\s|$|,|;)`)

// ShouldSuggestCoordinator returns true if the user message likely involves
// multi-file changes that would benefit from coordinator mode. This is a
// lightweight heuristic — not a guarantee.
func ShouldSuggestCoordinator(message string) bool {
	lower := strings.ToLower(message)

	// Korean multi-file keywords.
	koreanSignals := []string{
		"여러 파일", "다수의 파일", "파일들을", "전체적으로",
		"리팩토링", "대규모", "코디네이터",
	}
	for _, kw := range koreanSignals {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// English multi-file keywords.
	englishSignals := []string{
		"refactor across", "multiple files", "several files",
		"across the codebase", "coordinator mode", "coordinator",
	}
	for _, kw := range englishSignals {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// Count distinct file paths mentioned in the message.
	matches := filePathPattern.FindAllString(message, -1)
	uniquePaths := make(map[string]bool)
	for _, m := range matches {
		uniquePaths[strings.TrimSpace(m)] = true
	}
	return len(uniquePaths) >= 3
}
