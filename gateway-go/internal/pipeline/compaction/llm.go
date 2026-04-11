package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// keepRecentTurns is the minimum number of recent assistant turns to always
// preserve uncompacted. Old messages before this are candidates for summarization.
const keepRecentTurns = 6

// LLMCompact summarizes older messages using a local AI model when the context
// exceeds the configured threshold. Recent turns are preserved intact.
//
// Target: the summary should fit within LLMTargetPct of the context budget,
// leaving the rest for recent messages and new content.
func LLMCompact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, bool) {
	// Find split point: keep at least keepRecentTurns assistant turns.
	splitIdx := findSplitPoint(messages, keepRecentTurns)
	if splitIdx <= 1 {
		return messages, false // not enough old messages to compact
	}

	old := messages[:splitIdx]
	recent := messages[splitIdx:]

	// Serialize old messages for summarization.
	text := serializeMessages(old)
	if EstimateTokens(text) < 500 {
		return messages, false // too little to bother
	}

	// maxOutputTokens: target size for the summary, capped at 4096 to avoid
	// slow local AI responses. The structured prompt produces dense output
	// so 4096 tokens is sufficient for most compaction scenarios.
	maxOutput := int(float64(cfg.ContextBudget) * DefaultLLMTargetPct)
	if maxOutput > 4096 {
		maxOutput = 4096
	}
	summary, err := summarizer.Summarize(ctx, compactionSystemPrompt, text, maxOutput)
	if err != nil {
		if logger != nil {
			logger.Warn("polaris: LLM compaction failed", "error", err)
		}
		return messages, false
	}
	if summary == "" {
		return messages, false
	}

	// Rebuild: summary message + recent messages.
	compacted := make([]llm.Message, 0, 1+len(recent))
	compacted = append(compacted, llm.NewTextMessage("user",
		fmt.Sprintf("[Polaris compaction: %d messages summarized]\n\n%s", len(old), summary)))
	compacted = append(compacted, recent...)

	if logger != nil {
		logger.Info("polaris: LLM compaction applied",
			"oldMessages", len(old),
			"summaryTokens", EstimateTokens(summary),
			"recentMessages", len(recent))
	}
	return compacted, true
}

// findSplitPoint returns the message index that splits old (to compact) from
// recent (to preserve). Preserves at least keepTurns assistant turns at the end.
func findSplitPoint(messages []llm.Message, keepTurns int) int {
	turnsSeen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			turnsSeen++
			if turnsSeen >= keepTurns {
				return i
			}
		}
	}
	return 0
}

// serializeMessages converts messages to readable text for summarization.
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
					sb.WriteString(fmt.Sprintf("<tool: %s>", b.Name))
				case "tool_result":
					content := b.Content
					if len(content) > 800 {
						content = content[:800] + "..."
					}
					sb.WriteString(fmt.Sprintf("<result: %s>", content))
				}
				sb.WriteByte(' ')
			}
		} else {
			// Plain text content (JSON string).
			var text string
			if json.Unmarshal(msg.Content, &text) == nil {
				sb.WriteString(text)
			} else {
				sb.Write(msg.Content)
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// compactionSystemPrompt is the summarization prompt for Polaris compaction (Korean).
const compactionSystemPrompt = `아래 대화를 한국어로 요약하라.

- 구체적 사실(이름, 숫자, 날짜, 경로, 에러코드, 결정사항)을 빠짐없이 보존하라
- 완료/진행중 작업과 도구 실행 핵심 결과를 포함하라
- 고유명사와 코드는 원문 유지, 중복은 최신 값만 남겨라
- 불릿 위주로 간결하게, 사실 누락 금지`
