// wiki_recorder.go — Post-response diary recording (no LLM).
//
// After each successful agent run, the raw conversation turn is appended
// to today's diary file. No LLM summarization — just the original data.
//
// Wiki page curation (structured knowledge) is handled by the main LLM
// during its response turn via system prompt guidance.
package chat

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// shouldRecordDiary returns false for system-generated messages and noise
// that would pollute the diary without providing knowledge value.
func shouldRecordDiary(msg string, continuationIdx int) bool {
	// Skip autonomous continuation runs — system-generated, not user intent.
	if continuationIdx > 0 {
		return false
	}
	// Skip system-prefixed messages.
	if strings.HasPrefix(msg, "[System:") {
		return false
	}
	// Skip very short messages (likely pings/tests).
	trimmed := strings.TrimSpace(msg)
	if len([]rune(trimmed)) < 5 {
		return false
	}
	return true
}

// recordDiary appends the conversation turn to today's diary file.
// Called from handleRunSuccess as a background goroutine.
// No LLM needed — raw data is appended as-is.
func recordDiary(store *wiki.Store, logger *slog.Logger, userMsg string, toolNames []string, continuationIdx int) {
	if store == nil {
		return
	}
	if !shouldRecordDiary(userMsg, continuationIdx) {
		return
	}
	diaryDir := store.DiaryDir()
	if diaryDir == "" {
		return
	}

	// Record only the new input (user message + tools used).
	// The assistant response is derived content — skip to avoid bloat.
	var sb strings.Builder
	sb.WriteString(truncateDiary(userMsg, 500))
	if len(toolNames) > 0 {
		sb.WriteString(" [")
		sb.WriteString(strings.Join(toolNames, ", "))
		sb.WriteString("]")
	}

	if err := appendDiaryEntry(diaryDir, sb.String()); err != nil {
		logger.Warn("diary append failed", "error", err)
	}
}

// appendDiaryEntry appends a timestamped entry to today's diary file.
func appendDiaryEntry(diaryDir, content string) error {
	if content == "" {
		return fmt.Errorf("empty diary content")
	}
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return fmt.Errorf("diary dir: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(diaryDir, "diary-"+today+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("diary open: %w", err)
	}
	defer f.Close()

	now := time.Now().Format("15:04")
	entry := fmt.Sprintf("\n## %s\n\n%s\n", now, content)
	_, err = f.WriteString(entry)
	return err
}

func truncateDiary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
