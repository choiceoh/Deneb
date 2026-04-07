package coordinator

import (
	"regexp"
	"strings"
)

// filePathPattern matches common source file paths (e.g., "src/main.go", "internal/pipeline/chat/handler.go").
var filePathPattern = regexp.MustCompile(`(?:^|\s)[\w./\-]+\.\w{1,5}(?:\s|$|,|;)`)

// ShouldSuggestCoordinator returns true if the user message likely involves
// multi-file changes that would benefit from coordinator mode. This is a
// lightweight heuristic — not a guarantee.
func ShouldSuggestCoordinator(message string) bool {
	lower := strings.ToLower(message)

	// Korean multi-file / task-complexity keywords.
	koreanSignals := []string{
		"여러 파일", "다수의 파일", "파일들을", "전체적으로",
		"리팩토링", "대규모", "코디네이터",
		"전체 구조", "아키텍처", "모든 모듈", "일괄",
		"마이그레이션", "전면", "시스템 전체", "전부 바꿔",
		"동시에", "병렬로",
	}
	for _, kw := range koreanSignals {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// English multi-file / task-complexity keywords.
	englishSignals := []string{
		"refactor across", "multiple files", "several files",
		"across the codebase", "coordinator mode", "coordinator",
		"restructure", "migrate", "rename across", "all modules",
		"architecture", "system-wide", "bulk change", "in parallel",
	}
	for _, kw := range englishSignals {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	// Compound action patterns: multiple conjunction keywords suggest multi-step work.
	compoundKeywords := []string{"그리고", "다음에", "그 다음", "또한", "추가로"}
	compoundCount := 0
	for _, kw := range compoundKeywords {
		if strings.Contains(lower, kw) {
			compoundCount++
		}
	}
	if compoundCount >= 2 {
		return true
	}

	// Count distinct file paths mentioned in the message.
	matches := filePathPattern.FindAllString(message, -1)
	uniquePaths := make(map[string]struct{})
	for _, m := range matches {
		uniquePaths[strings.TrimSpace(m)] = struct{}{}
	}
	return len(uniquePaths) >= 2
}
