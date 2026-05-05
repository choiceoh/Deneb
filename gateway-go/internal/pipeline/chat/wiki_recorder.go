// wiki_recorder.go — Post-response diary recording (no LLM).
//
// After each successful agent run, the raw conversation turn is appended
// to today's diary file. No LLM summarization — just the original data.
//
// Wiki page curation (structured knowledge) is handled by the main LLM
// during its response turn via system prompt guidance.
package chat

import (
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type diarySignal struct {
	Level  string
	Reason string
}

var durableDiaryTerms = []string{
	"결정", "계획", "구현", "수정", "테스트", "검증", "오류", "버그", "회상", "기억", "컴팩션", "위키", "일지",
	"도구", "파일", "머지", "설정", "배포", "리팩토링", "분석", "정리", "요약", "추가", "개선", "완료", "실패", "차단",
	"pr", "merge", "fix", "bug", "test", "plan", "memory", "recall", "compaction", "wiki", "error", "config",
}

var trivialDiaryMessages = map[string]struct{}{
	"응": {}, "ㅇㅇ": {}, "네": {}, "넵": {}, "예": {}, "아니": {}, "아니요": {},
	"고마워": {}, "감사": {}, "감사해": {}, "땡큐": {}, "좋아": {}, "굿": {}, "확인": {},
	"알겠어": {}, "오케이": {}, "ok": {}, "okay": {}, "thanks": {}, "thank you": {}, "thx": {},
	"ㅋㅋ": {}, "ㅎㅎ": {},
}

// shouldRecordDiary returns false for system-generated messages and noise
// that would pollute the diary without providing knowledge value.
func shouldRecordDiary(msg string, signal diarySignal) bool {
	// Skip system-prefixed messages.
	if strings.HasPrefix(msg, "[System:") {
		return false
	}
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" || signal.Level == "" {
		return false
	}
	if _, ok := trivialDiaryMessages[strings.ToLower(trimmed)]; ok && signal.Level == "low" {
		return false
	}
	// Skip very short messages unless the assistant/tool outcome carries
	// durable signal (e.g. "ㄱㄱ" followed by a concrete implementation result).
	if utf8.RuneCountInString(trimmed) < 5 && signal.Level == "low" {
		return false
	}
	return true
}

func shouldRecordRunDiary(params RunParams) bool {
	if strings.TrimSpace(params.Message) == "" {
		return false
	}
	if params.EphemeralUser {
		return false
	}
	if isSystemSession(params.SessionKey) {
		return false
	}
	return true
}

// recordDiary appends the conversation turn to today's diary file.
// Called from handleRunSuccess as a background goroutine.
// No LLM needed — raw data is appended as-is.
func recordDiary(store *wiki.Store, logger *slog.Logger, userMsg string, toolNames []string, assistantText, stopReason string, turns int) bool {
	if store == nil {
		return false
	}
	signal := classifyDiarySignal(userMsg, toolNames, assistantText)
	if !shouldRecordDiary(userMsg, signal) {
		return false
	}
	diaryDir := store.DiaryDir()
	if diaryDir == "" {
		return false
	}

	// Record a compact outcome capsule. Tool outputs stay in the transcript;
	// the diary only needs enough signal for later wiki consolidation.
	var sb strings.Builder
	sb.WriteString("사용자: ")
	sb.WriteString(truncateDiary(userMsg, 500))
	sb.WriteString("\n신호: ")
	sb.WriteString(signal.Level)
	if signal.Reason != "" {
		sb.WriteString("/")
		sb.WriteString(signal.Reason)
	}
	if len(toolNames) > 0 {
		sb.WriteString("\n도구: ")
		sb.WriteString(strings.Join(toolNames, ", "))
	}
	if strings.TrimSpace(assistantText) != "" {
		sb.WriteString("\n결과: ")
		sb.WriteString(truncateDiary(assistantText, 900))
	}
	if stopReason != "" || turns > 0 {
		sb.WriteString("\n상태: ")
		if stopReason != "" {
			sb.WriteString("stop=")
			sb.WriteString(stopReason)
		}
		if turns > 0 {
			if stopReason != "" {
				sb.WriteString(", ")
			}
			sb.WriteString("turns=")
			sb.WriteString(strconv.Itoa(turns))
		}
	}

	if err := store.AppendDiary(sb.String()); err != nil {
		if logger != nil {
			logger.Warn("diary append failed", "error", err)
		}
		return false
	}
	return true
}

func classifyDiarySignal(userMsg string, toolNames []string, assistantText string) diarySignal {
	userMsg = strings.TrimSpace(userMsg)
	assistantText = strings.TrimSpace(assistantText)
	if len(toolNames) > 0 {
		return diarySignal{Level: "action", Reason: "tools"}
	}
	if containsDurableDiaryTerm(userMsg) || containsDurableDiaryTerm(assistantText) {
		return diarySignal{Level: "durable", Reason: "keyword"}
	}
	if utf8.RuneCountInString(userMsg) >= 24 || utf8.RuneCountInString(assistantText) >= 160 {
		return diarySignal{Level: "context", Reason: "substantial"}
	}
	if assistantText != "" {
		return diarySignal{Level: "low", Reason: "brief-outcome"}
	}
	return diarySignal{}
}

func containsDurableDiaryTerm(text string) bool {
	lower := strings.ToLower(text)
	for _, term := range durableDiaryTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func truncateDiary(s string, maxLen int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
