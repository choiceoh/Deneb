package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// Lightweight-LLM card titler for mail reports.
//
// The analysis (main) model writes a generic "메일 분석 리포트" heading; the work-feed
// card should instead carry the email's real subject. Naming a mail is a tiny job,
// so it is handed to the lightweight role (local, cheap, fast) rather than the
// analysis model. Best-effort: any failure returns "" and proactive_relay falls
// back to the deterministic extractCardTitle subject heuristic.

const (
	// mailTitleMaxInputRunes bounds the report text sent to the titler. The
	// subject sits at the very top of a report, so the head is plenty and keeps
	// the call cheap.
	mailTitleMaxInputRunes = 1200
	// mailTitleMaxTokens caps the generated title length.
	mailTitleMaxTokens = 48
	// mailTitleTimeout bounds the lightweight call so a stalled model never holds
	// up proactive delivery.
	mailTitleTimeout = 8 * time.Second
)

const mailTitleSystemPrompt = `너는 업무 메일 분석 리포트에서 카드 제목 한 줄만 뽑아내는 도구다.
출력 규칙:
- 그 메일이 무엇에 관한 것인지 나타내는 짧은 한국어 명사구 한 줄만 출력한다 (메일의 제목/핵심 주제).
- 30자 이내. 따옴표·마크다운·머리기호·이모지 금지.
- "메일 분석", "리포트", "보고" 같은 군더더기 단어를 붙이지 마라.
- 설명·접두어 없이 제목 문자열만 출력한다.`

// mailCardTitle returns a lightweight-model-generated card title for a mail
// report body, or "" on any failure (so the heuristic fallback applies). It is
// wired as proactiveRelayDeps.cardTitler.
func (s *Server) mailCardTitle(content string) string {
	body := content
	if r := []rune(body); len(r) > mailTitleMaxInputRunes {
		body = string(r[:mailTitleMaxInputRunes])
	}
	ctx, cancel := context.WithTimeout(s.ShutdownCtx(), mailTitleTimeout)
	defer cancel()
	out, err := pilot.CallRoleLLM(ctx, modelrole.RoleLightweight, mailTitleSystemPrompt, body, mailTitleMaxTokens)
	if err != nil {
		return ""
	}
	return cleanLLMCardTitle(out)
}

// cleanLLMCardTitle normalizes a raw lightweight-model title into a card-ready
// string: first line, markdown/quotes stripped, clipped. Returns "" when the
// model declined or echoed a generic "메일 분석 리포트" label, so the caller falls
// back to the heuristic subject.
func cleanLLMCardTitle(raw string) string {
	line := strings.TrimSpace(raw)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = stripMarkdownLine(line)
	line = strings.Trim(line, " \t\"'`“”‘’「」『』")
	line = strings.TrimSpace(line)
	if len([]rune(line)) < 3 || isGenericMailReportTitle(line) {
		return ""
	}
	return clipRunes(line, workFeedTitleMaxRunes)
}
