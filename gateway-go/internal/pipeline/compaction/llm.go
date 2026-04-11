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

// chunkMaxTokens is the threshold above which old messages are split into
// smaller chunks before summarization. Chunking avoids lost-in-the-middle
// degradation when serialized input is very large.
const chunkMaxTokens = 20_000

// LLMCompact summarizes older messages using a local AI model when the context
// exceeds the configured threshold. Recent turns are preserved intact.
//
// When old messages exceed chunkMaxTokens, they are split into ≤chunkMaxTokens
// chunks and summarized in parallel, then joined. This prevents quality
// degradation from feeding excessively long input to the local model.
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

	// maxOutput: proportional to context budget, no arbitrary cap.
	// Larger context deserves a proportionally larger summary budget.
	maxOutput := int(float64(cfg.ContextBudget) * DefaultLLMTargetPct)

	var summary string
	if EstimateMessagesTokens(old) > chunkMaxTokens {
		var ok bool
		summary, ok = summarizeInChunks(ctx, old, summarizer, maxOutput, logger)
		if !ok {
			return messages, false
		}
	} else {
		text := serializeMessages(old)
		if EstimateTokens(text) < 500 {
			return messages, false // too little to bother
		}
		var err error
		summary, err = summarizer.Summarize(ctx, compactionSystemPrompt, text, maxOutput)
		if err != nil {
			if logger != nil {
				logger.Warn("polaris: LLM compaction failed", "error", err)
			}
			return messages, false
		}
		if summary == "" {
			return messages, false
		}
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

// summarizeInChunks splits old messages into ≤chunkMaxTokens chunks,
// summarizes them in parallel, and joins the results in order.
// perChunkOutput = maxOutput / numChunks (floor 1024) to keep total bounded.
func summarizeInChunks(
	ctx context.Context,
	old []llm.Message,
	summarizer Summarizer,
	maxOutput int,
	logger *slog.Logger,
) (string, bool) {
	chunks := splitIntoChunks(old, chunkMaxTokens)
	perChunkOutput := maxOutput / len(chunks)
	if perChunkOutput < 1024 {
		perChunkOutput = 1024
	}

	type chunkResult struct {
		idx     int
		summary string
		err     error
	}
	resultCh := make(chan chunkResult, len(chunks))

	for i, chunk := range chunks {
		go func(idx int, msgs []llm.Message) {
			text := serializeMessages(msgs)
			if EstimateTokens(text) < 100 {
				resultCh <- chunkResult{idx: idx}
				return
			}
			s, err := summarizer.Summarize(ctx, compactionSystemPrompt, text, perChunkOutput)
			resultCh <- chunkResult{idx: idx, summary: s, err: err}
		}(i, chunk)
	}

	results := make([]string, len(chunks))
	for range chunks {
		r := <-resultCh
		if r.err != nil {
			if logger != nil {
				logger.Warn("polaris: chunk summarization failed",
					"chunk", r.idx, "total", len(chunks), "error", r.err)
			}
			return "", false
		}
		results[r.idx] = r.summary
	}

	var parts []string
	for _, s := range results {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n\n"), true
}

// splitIntoChunks groups messages into batches of ≤maxTokens tokens each.
func splitIntoChunks(messages []llm.Message, maxTokens int) [][]llm.Message {
	var chunks [][]llm.Message
	var current []llm.Message
	currentTokens := 0

	for _, msg := range messages {
		msgTokens := EstimateTokens(string(msg.Content)) + 4
		if len(current) > 0 && currentTokens+msgTokens > maxTokens {
			chunks = append(chunks, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, msg)
		currentTokens += msgTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
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

// compactionSystemPrompt is the structured summarization prompt (Korean).
const compactionSystemPrompt = `아래 대화 내용을 정해진 형식으로 요약하라. 반드시 모든 섹션을 작성해야 한다.

## 규칙
- 모든 구체적 사실(이름, 숫자, 날짜, IP, 코드명, 에러코드, 경로 등)을 빠짐없이 기록
- 사실이 수정된 경우 수정된 값만 기록 (원래 값 삭제)
- 도구 실행 결과에서 핵심 데이터 추출하여 기록
- 한국어로 작성 (고유명사/코드는 원문 유지)
- 가능한 한 간결하게 작성하되 사실을 누락하지 마라

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
