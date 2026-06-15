package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// Tiny-LLM card titler for proactive work-feed cards.
//
// The analysis (main) model often writes a generic heading ("메일 분석 리포트") or
// opens with a narration sentence, so the deterministic heuristic ends up with a
// poor card title (a generic label, or a whole sentence). Naming a card is a tiny
// job — a short Korean noun-phrase title — so it is handed to the TINY role
// (diffusiongemma: local, fast, and validated production-grade on exactly this
// title-summary task) rather than the larger lightweight/analysis models. Same
// task shape as session auto-titling, which already uses tiny. Used both for mail
// reports (to surface the email's real subject) and for any proactive body whose
// heuristic title is weak (see isWeakCardTitle). Best-effort: any failure returns
// "" and proactive_relay falls back to the deterministic extractCardTitle heuristic.

const (
	// cardTitleMaxInputRunes bounds the report text sent to the titler. The subject
	// / topic sits at the top of a report, so the head is plenty and keeps the call
	// cheap.
	cardTitleMaxInputRunes = 1200
	// cardTitleMaxTokens caps the generated title length.
	cardTitleMaxTokens = 48
	// cardTitleTimeout bounds the tiny-model call so a stalled model never holds
	// up proactive delivery.
	cardTitleTimeout = 8 * time.Second
)

const cardTitleSystemPrompt = `너는 업무 알림 카드의 제목 한 줄만 뽑아내는 도구다. 입력은 메일 분석 리포트, 일정 브리핑, 분석 메모 등 다양하다.
출력 규칙:
- 그 알림이 무엇에 관한 것인지 나타내는 짧은 한국어 명사구 한 줄만 출력한다 (핵심 주제/제목).
- 30자 이내. 따옴표·마크다운·머리기호·이모지 금지.
- "메일 분석", "리포트", "보고" 같은 군더더기 단어를 붙이지 마라.
- 문장 종결("~합니다", "~다")·설명·접두어 없이 제목 문자열만 출력한다.`

// cardTitle returns a tiny-model-generated card title for a proactive body, or ""
// on any failure (so the heuristic fallback applies). It is wired as
// proactiveRelayDeps.cardTitler.
func (s *Server) cardTitle(content string) string {
	body := content
	if r := []rune(body); len(r) > cardTitleMaxInputRunes {
		body = string(r[:cardTitleMaxInputRunes])
	}
	ctx, cancel := context.WithTimeout(s.ShutdownCtx(), cardTitleTimeout)
	defer cancel()
	out, err := pilot.CallRoleLLM(ctx, modelrole.RoleTiny, cardTitleSystemPrompt, body, cardTitleMaxTokens)
	if err != nil {
		return ""
	}
	return cleanLLMCardTitle(out)
}

// cleanLLMCardTitle normalizes a raw tiny-model title into a card-ready
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
