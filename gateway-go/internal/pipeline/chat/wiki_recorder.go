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

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

type diarySignal struct {
	Level  string
	Reason string
}

// preference reports whether the capsule was tagged with the 선호 reason — the
// standing-preference signal the dreamer's 사용자 synthesis keys on. Callers use
// it to nudge the dreamer onto its accelerated preference cadence.
func (s diarySignal) preference() bool {
	return strings.Contains(s.Reason, "선호")
}

// project reports whether the capsule carried a deal-number cue (견적/단가/
// 카톡 보고). Callers use it to nudge the dreamer onto the same accelerated
// cadence as 선호. Bare "프로젝트 근황" must not set this.
func (s diarySignal) project() bool {
	return strings.Contains(s.Reason, "프로젝트시그널")
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
	// Autonomous cron runs feed the wiki through their own sinks (goal ledger,
	// mail analyses, direct wiki writes), and their "user" message is a payload
	// prompt, not the user speaking — diarying them would double-feed dreams.
	// (This exclusion is inert on the async lifecycle path: cron: turns only
	// ever run through the sync entry points.)
	if session.IsCronSession(params.SessionKey) {
		return false
	}
	return true
}

// recordDiary appends the conversation turn to today's diary file.
// Called from handleRunSuccess as a background goroutine.
// No LLM needed — raw data is appended as-is. The classified signal is
// returned alongside so the caller can propagate preference cues (선호) to the
// dreamer's accelerated cadence.
func recordDiary(store *tooldeps.WikiStore, logger *slog.Logger, userMsg string, toolNames []string, assistantText, stopReason string, turns int) (bool, diarySignal) {
	if store == nil {
		return false, diarySignal{}
	}
	signal := classifyDiarySignal(userMsg, toolNames, assistantText)
	if !shouldRecordDiary(userMsg, signal) {
		return false, signal
	}
	diaryDir := store.DiaryDir()
	if diaryDir == "" {
		return false, signal
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
		return false, signal
	}
	return true, signal
}

func classifyDiarySignal(userMsg string, toolNames []string, assistantText string) diarySignal {
	userMsg = strings.TrimSpace(userMsg)
	assistantText = strings.TrimSpace(assistantText)
	// A standing preference / style correction is the behavioral signal the
	// dreamer abstracts into 사용자 working-style rules. Tag it explicitly ("선호")
	// so the dreamer aggregates a clear cue instead of inferring it from raw text,
	// and force-record it (durable) even when the message is short.
	pref := isPreferenceDirective(userMsg)
	proj := isProjectDealSignal(userMsg) || isProjectDealSignal(assistantText)
	if len(toolNames) > 0 {
		reason := "tools"
		if pref {
			reason = "tools,선호"
		}
		if proj {
			reason += ",프로젝트시그널"
		}
		return diarySignal{Level: "action", Reason: reason}
	}
	if pref {
		reason := "선호"
		if proj {
			reason += ",프로젝트시그널"
		}
		return diarySignal{Level: "durable", Reason: reason}
	}
	if proj {
		return diarySignal{Level: "durable", Reason: "프로젝트시그널"}
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

// preferenceDiaryTerms flag a user turn that voices a STANDING preference, a
// style/format correction, or an explicit remember-this — the behavioral signal
// the dreamer abstracts into 사용자 (user) working-style rules and consolidates
// on its accelerated preference cadence. High-precision phrasing: standing
// directives, replacement/negation, and style corrections, not one-off task
// words. A mis-tag is low-harm — the dreamer's rules only record what was
// clearly standing or repeated — so the list favors catching genuine preference
// cues over avoiding the odd false positive.
var preferenceDiaryTerms = []string{
	"앞으로", "항상", "매번", "다음부터", "다음부턴", "이제부터", "기본으로", "기본값", // standing directives
	"그만", "하지 마", "하지마", "말고", "대신", // negation / replacement
	"산문으로", "불릿", "간결", "짧게", "자세히", "줄여", // style / format
	"말투", "호칭", "존댓말", "반말", "포맷", // tone / address / format corrections
	"선호", "취향", "스타일로", // explicit preference
	"기억해둬", "기억해 둬", "저장해둬", "잊지 마", "잊지마", "명심", // explicit remember-this
}

// isPreferenceDirective reports whether a user message voices a standing
// preference or a style/format correction (see preferenceDiaryTerms).
func isPreferenceDirective(userMsg string) bool {
	lower := strings.ToLower(strings.TrimSpace(userMsg))
	for _, t := range preferenceDiaryTerms {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// projectDiaryTerms are high-precision deal-number cues. Kept in lockstep
// with wiki.projectSignalTerms — a loose "프로젝트" match would fire on
// every status question and thrash the 30-minute dream cadence.
var projectDiaryTerms = []string{
	"견적", "재견적", "단가", "moq", "공급가액", "원/w", "원/wp",
	"카톡 보고", "카톡보고",
}

func isProjectDealSignal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, t := range projectDiaryTerms {
		if strings.Contains(lower, t) {
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
