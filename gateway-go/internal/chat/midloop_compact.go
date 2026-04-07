// midloop_compact.go provides emergency summarization for context overflow.
//
// The main chat pipeline rarely hits context limits because RLM handles
// tool-heavy iterative work in its own context. Emergency summarization is
// the last resort when context_length_exceeded occurs: instead of blindly
// dropping middle messages (losing all facts), we summarize them first.
//
// Uses a structured template with dedicated sections for facts, progress,
// decisions, and tool outputs to force fact preservation during summarization
// (Factory.ai / Knowledge Objects pattern).
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	compact "github.com/choiceoh/deneb/gateway-go/internal/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// emergencySummaryMaxTokens caps the LLM summary size during emergency compaction.
const emergencySummaryMaxTokens = 4096

// emergencySummarize attempts to produce a structured summary of messages
// that are about to be dropped due to context overflow. Returns empty string
// on failure (caller falls back to dropping without summary).
func emergencySummarize(
	ctx context.Context,
	client agent.LLMStreamer,
	model string,
	messages []llm.Message,
	logger *slog.Logger,
) string {
	if len(messages) == 0 {
		return ""
	}

	// Strip images to reduce the summarization request size.
	messages = compact.StripImageBlocks(messages)

	conversationText := serializeMessages(messages)

	prompt := emergencyCompactionPrompt + "\n\n## 요약할 대화 내용\n\n" + conversationText

	text, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: emergencySummaryMaxTokens,
	})
	if err != nil {
		logger.Warn("emergency summarization failed, dropping without summary", "error", err)
		return ""
	}
	if text == "" {
		logger.Warn("emergency summarization returned empty")
		return ""
	}

	logger.Info("emergency summarization succeeded", "summaryLen", len(text))
	return text
}

// emergencyCompactionPrompt is the structured summarization prompt.
// Dedicated sections force fact preservation (Factory.ai pattern).
const emergencyCompactionPrompt = `아래 대화 내용을 정해진 형식으로 요약하라. 반드시 모든 섹션을 작성해야 한다.

## 규칙
- 모든 구체적 사실(이름, 숫자, 날짜, IP, 코드명, 에러코드, 경로 등)을 빠짐없이 기록
- 사실이 수정된 경우 수정된 값만 기록 (원래 값 삭제)
- 도구 실행 결과에서 핵심 데이터 추출하여 기록
- 한국어로 작성 (고유명사/코드는 원문 유지)

## 출력 형식 (이 구조를 정확히 따르라)

### 핵심 사실 (Facts)
유저가 알려준 정보, 시스템에서 확인된 사실을 개별 항목으로:
- [카테고리] 항목: 값

### 진행 상황 (Progress)
완료/진행중/차단된 작업:
- [완료] 작업 설명
- [진행중] 작업 설명

### 결정 사항 (Decisions)
유저가 내린 결정이나 선택:
- 결정 내용 (이유)

### 도구 실행 결과 (Tool Outputs)
도구가 반환한 핵심 데이터:
- [도구명] 결과 요약

### 대화 맥락 (Context)
대화의 흐름과 유저의 의도 요약 (2-3문장)`

// serializeMessages converts a slice of LLM messages into a readable text
// representation for summarization.
func serializeMessages(messages []llm.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%s]: ", msg.Role))
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err == nil {
			for _, b := range blocks {
				switch b.Type {
				case "text":
					sb.WriteString(b.Text)
				case "tool_use":
					sb.WriteString(fmt.Sprintf("<tool: %s, input: ", b.Name))
					if len(b.Input) > 200 {
						sb.Write(b.Input[:200])
						sb.WriteString("...")
					} else {
						sb.Write(b.Input)
					}
					sb.WriteByte('>')
				case "tool_result":
					content := b.Content
					if len(content) > 1000 {
						content = content[:1000] + "..."
					}
					sb.WriteString(fmt.Sprintf("<result: %s>", content))
				}
				sb.WriteByte(' ')
			}
		} else {
			sb.Write(msg.Content)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
