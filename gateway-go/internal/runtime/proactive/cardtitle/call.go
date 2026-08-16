package cardtitle

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

const (
	cardTitleMaxTokens = 256
	cardTitleTimeout   = 8 * time.Second
)

const systemPrompt = `너는 업무 알림 카드의 제목과 짧은 요약을 뽑아내는 도구다. 입력은 메일 분석 리포트, 일정 브리핑, 분석 메모 등이다.
정확히 아래 두 줄 형식으로만 출력한다:
제목: <핵심 명사구>
요약: <무엇에 관한 것이고 왜 중요한지>

규칙:
- 제목은 한글 20자 이내. "메일 분석", "리포트", "보고" 같은 군더더기 단어를 붙이지 마라.
- 요약은 카드 미리보기용으로 2문장(약 80자) 이내. 제목을 그대로 반복하지 말고 핵심 내용과 이유를 담는다.
- 따옴표·마크다운·머리기호·이모지 금지. 위 두 줄 외에 다른 설명·접두어를 출력하지 마라.`

var (
	// Labels may sit mid-line after a reasoning preamble the model leaked
	// into the content channel ("We need … 제목: 풍력 실측").
	titleLabelRe   = regexp.MustCompile(`(?i)(?:^|[\s，,。．;；:：])(제목|title)\s*[:：]\s*`)
	summaryLabelRe = regexp.MustCompile(`(?i)(?:^|[\s，,。．;；:：])(요약|summary)\s*[:：]\s*`)
)

// CallTiny asks the tiny role for a raw title/summary response.
func CallTiny(ctx context.Context, body string) (raw string, err error) {
	callCtx, cancel := context.WithTimeout(ctx, cardTitleTimeout)
	defer cancel()
	return pilot.CallTinyLLM(callCtx, systemPrompt, body, cardTitleMaxTokens)
}

// EvaluateCardTitleRole is the live-test entry for card-title role evaluation.
func EvaluateCardTitleRole(ctx context.Context, role modelrole.Role, content string) (title, summary, raw string, err error) {
	callCtx, cancel := context.WithTimeout(ctx, cardTitleTimeout)
	defer cancel()
	if role == modelrole.RoleTiny {
		raw, err = pilot.CallTinyLLM(callCtx, systemPrompt, content, cardTitleMaxTokens)
	} else {
		raw, err = pilot.CallRoleLLM(callCtx, role, systemPrompt, content, cardTitleMaxTokens)
	}
	if err != nil {
		return "", "", raw, err
	}
	title, summary = ParseLLMTitleSummary(raw)
	return title, summary, raw, nil
}

// ParseLLMTitleSummary extracts title/summary from labeled model output only.
// Unlabeled first lines are ignored — reasoning models often dump chain-of-thought
// there ("We need answer in Korean…"), and that must not become the card title.
func ParseLLMTitleSummary(raw string) (title, summary string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if loc := titleLabelRe.FindStringIndex(line); loc != nil && title == "" {
			rest := strings.TrimSpace(line[loc[1]:])
			if sloc := summaryLabelRe.FindStringIndex(rest); sloc != nil {
				title = strings.TrimSpace(rest[:sloc[0]])
				summary = strings.TrimSpace(rest[sloc[1]:])
				continue
			}
			title = rest
			continue
		}
		if loc := summaryLabelRe.FindStringIndex(line); loc != nil {
			summary = strings.TrimSpace(line[loc[1]:])
		}
	}
	return strings.TrimSpace(title), strings.TrimSpace(summary)
}
