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

// maxChunksPerPass bounds how many chunks a single compaction pass summarizes.
// Compaction runs STW under a shared ~2-minute deadline; an unbounded fan-out
// (30 chunks on a 600K-token backlog, 2026-06-05) meant the slowest chunks
// always exceeded that shared deadline and the whole pass was discarded, so no
// progress was ever made. Bounding the batch keeps each pass's wall time
// predictable; the uncovered remainder stays raw and the next pass digests the
// following batch (the summary DAG accumulates leaf nodes per pass).
const maxChunksPerPass = 4

// LLMCompact summarizes older messages using a local AI model when the context
// exceeds the configured threshold. Recent turns are preserved intact.
//
// When old messages exceed chunkMaxTokens, they are split into ≤chunkMaxTokens
// chunks and at most maxChunksPerPass of the oldest chunks are summarized in
// parallel per pass. Messages the summary does not cover (later chunks, or
// chunks past a failed one) are kept raw between the summary and the recent
// tail, so a later pass can digest them — partial progress instead of
// all-or-nothing.
func LLMCompact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) ([]llm.Message, string, bool) {
	// Find split point: keep at least keepRecentTurns assistant turns.
	splitIdx := findSplitPoint(messages, keepRecentTurns)
	if splitIdx <= 1 {
		return messages, "", false // not enough old messages to compact
	}

	old := messages[:splitIdx]
	recent := messages[splitIdx:]

	summary, covered := summarizeOldMessages(ctx, cfg, old, summarizer, logger)
	if summary == "" || covered <= 0 {
		return messages, "", false
	}
	leftover := old[covered:] // uncovered old stays raw for the next pass

	// Rebuild: summary message + uncovered old + recent messages. The returned
	// summary is surfaced via Result.Summary so the caller can persist it (DAG
	// leaf node) or feed it back as Config.PreviousSummary next time.
	compacted := make([]llm.Message, 0, 1+len(leftover)+len(recent))
	compacted = append(compacted, llm.NewTextMessage("user",
		FormatContextFence(
			"polaris-compaction",
			"conversation-summary",
			fmt.Sprintf("Polaris compaction: %d messages summarized", covered),
			summary,
		)))
	compacted = append(compacted, leftover...)
	compacted = append(compacted, recent...)

	if logger != nil {
		logger.Info("polaris: LLM compaction applied",
			"oldMessages", len(old),
			"coveredMessages", covered,
			"leftoverRaw", len(leftover),
			"summaryTokens", EstimateTokens(summary),
			"recentMessages", len(recent),
			"incremental", strings.TrimSpace(cfg.PreviousSummary) != "")
	}
	return compacted, summary, true
}

// summarizeOldMessages produces an LLM summary of a prefix of the given
// messages and reports how many leading messages it covers. maxOutput is
// cfg.ContextBudget × DefaultLLMTargetPct (no arbitrary cap). When input
// exceeds chunkMaxTokens it is summarized in bounded parallel chunk batches,
// so covered may be smaller than len(old). Returns ("", 0) when summarization
// was skipped (too little content) or failed.
func summarizeOldMessages(
	ctx context.Context,
	cfg Config,
	old []llm.Message,
	summarizer Summarizer,
	logger *slog.Logger,
) (string, int) {
	maxOutput := int(float64(cfg.ContextBudget) * DefaultLLMTargetPct)
	hasPrev := strings.TrimSpace(cfg.PreviousSummary) != ""

	systemPrompt := compactionPrompt(compactionSystemPrompt, cfg)
	if hasPrev {
		systemPrompt = compactionPrompt(recompactionSystemPrompt, cfg)
	}

	if EstimateMessagesTokens(old) > chunkMaxTokens {
		// An incremental update normally folds the (small) prior summary + new
		// turns into one call, but when the new turns alone exceed a chunk that
		// single call would blow the summarizer's input. Fall back to fresh
		// chunked summarization: the prior summary stays wherever the caller
		// keeps it, and the raw turns are still present, so facts are
		// re-derived from raw rather than lost — just less incremental.
		if hasPrev {
			if logger != nil {
				logger.Info("polaris: incremental update input too large, using fresh chunked path",
					"oldTokens", EstimateMessagesTokens(old))
			}
			systemPrompt = compactionPrompt(compactionSystemPrompt, cfg)
		}
		return summarizeInChunks(ctx, old, summarizer, maxOutput, systemPrompt, logger)
	}

	text := serializeMessages(old)
	if hasPrev {
		// Feed the prior summary alongside the new turns so the model UPDATES
		// it (In Progress → Done, refresh state) rather than re-summarizing.
		text = "## 이전 요약 (이것을 갱신하라)\n" + cfg.PreviousSummary + "\n\n## 새 대화 (반영할 변경)\n" + text
	} else if EstimateTokens(text) < 500 {
		return "", 0 // too little to bother (fresh path only)
	}

	summary, err := summarizer.Summarize(ctx, systemPrompt, text, maxOutput)
	if err = summarizeCallErr(ctx, err); err != nil {
		if logger != nil {
			logger.Warn("polaris: LLM compaction failed", "error", err)
		}
		return "", 0
	}
	return summary, len(old)
}

// summarizeCallErr normalizes a Summarize result: a nil error with the parent
// context already expired means the underlying stream was likely cut
// mid-output and the returned text truncated (pilot.CollectStream returns the
// partial text with a nil error on ctx.Done). Persisting a truncated summary
// as covering its message range would silently lose facts, so surface the
// context error and let the caller retry that range on a later pass.
func summarizeCallErr(ctx context.Context, err error) error {
	if err != nil {
		return err
	}
	return ctx.Err()
}

// compactionPrompt applies both soft-hint augmentations (anchors, then learned
// guidelines) to a base summarization prompt. Single choke point so every
// compaction path — fresh, recompaction, chunked, emergency — emphasizes the
// same preservation hints.
func compactionPrompt(base string, cfg Config) string {
	return augmentWithGuidelines(augmentWithAnchors(base, cfg.AnchorKeywords), cfg.LearnedGuidelines)
}

// augmentWithAnchors appends an anchor preservation instruction to the base
// summarization prompt when keywords are present. Soft hint — the summarizer
// is asked to preserve facts related to these keywords as inevictable.
func augmentWithAnchors(base string, anchors []string) string {
	if len(anchors) == 0 {
		return base
	}
	return base + "\n\n## 필수 보존 키워드 (Anchor)\n다음 키워드와 관련된 사실은 절대 누락하지 말고 보존하라:\n- " + strings.Join(anchors, "\n- ")
}

// augmentWithGuidelines appends learned preservation guidelines (distilled from
// past compaction misses) to the base prompt. Additive soft hints — they tell
// the summarizer what categories of detail to keep, learned from cases where a
// compacted summary later proved insufficient.
func augmentWithGuidelines(base string, guidelines []string) string {
	if len(guidelines) == 0 {
		return base
	}
	return base + "\n\n## 학습된 보존 지침 (과거 압축 누락에서 도출)\n과거 요약에서 빠져 문제가 됐던 항목들이다. 다음 종류의 정보를 특히 보존하라:\n- " + strings.Join(guidelines, "\n- ")
}

// summarizeInChunks splits old messages into ≤chunkMaxTokens chunks,
// summarizes the oldest ≤maxChunksPerPass of them in parallel, and joins the
// results in order. Returns the joined summary and the number of leading
// messages it covers. perChunkOutput = maxOutput / numChunks (floor 1024).
//
// Failure tolerance: a failed chunk no longer discards the whole pass. The
// longest contiguous prefix of successful chunks is kept — coverage must stay
// gapless because the caller records a single covered range — and everything
// from the first failure on stays raw for a later pass. Only a chunk-0
// failure yields ("", 0).
func summarizeInChunks(
	ctx context.Context,
	old []llm.Message,
	summarizer Summarizer,
	maxOutput int,
	systemPrompt string,
	logger *slog.Logger,
) (string, int) {
	chunks := splitIntoChunks(old, chunkMaxTokens)
	if len(chunks) > maxChunksPerPass {
		if logger != nil {
			logger.Info("polaris: chunk batch limited",
				"totalChunks", len(chunks), "digested", maxChunksPerPass)
		}
		chunks = chunks[:maxChunksPerPass]
	}
	// Cap per-chunk output: generation time, not prefill, dominates a chunk
	// call (live: ~1.4K-token summaries ≈ 30-45s each on the analysis model),
	// so an uncapped maxOutput/len share (~7K at a 140K budget) can single-
	// handedly blow the shared STW deadline. 2048 ≈ 10:1 compression per
	// chunk and double the floor — summaries stay substantive while a full
	// batch reliably finishes within the deadline.
	perChunkOutput := maxOutput / len(chunks)
	if perChunkOutput < 1024 {
		perChunkOutput = 1024
	}
	if perChunkOutput > 2048 {
		perChunkOutput = 2048
	}

	type chunkResult struct {
		idx     int
		summary string
		err     error
	}
	resultCh := make(chan chunkResult, len(chunks))

	for i, chunk := range chunks {
		go func(idx int, msgs []llm.Message) {
			// A panic here must still deliver a result: the collector below
			// reads exactly len(chunks) results, so a dead goroutine would
			// wedge the whole compaction pass forever (and kill the process
			// without the recover).
			defer func() {
				if r := recover(); r != nil {
					if logger != nil {
						logger.Error("panic in chunk summarization", "chunk", idx, "panic", r)
					}
					resultCh <- chunkResult{idx: idx, err: fmt.Errorf("chunk summarizer panic: %v", r)}
				}
			}()
			text := serializeMessages(msgs)
			if EstimateTokens(text) < 100 {
				resultCh <- chunkResult{idx: idx}
				return
			}
			s, err := summarizer.Summarize(ctx, systemPrompt, text, perChunkOutput)
			resultCh <- chunkResult{idx: idx, summary: s, err: summarizeCallErr(ctx, err)}
		}(i, chunk)
	}

	results := make([]string, len(chunks))
	failed := make([]bool, len(chunks))
	for range chunks {
		r := <-resultCh
		if r.err != nil {
			failed[r.idx] = true
			if logger != nil {
				logger.Warn("polaris: chunk summarization failed",
					"chunk", r.idx, "total", len(chunks), "error", r.err)
			}
			continue
		}
		results[r.idx] = r.summary
	}

	okChunks := 0
	for okChunks < len(chunks) && !failed[okChunks] {
		okChunks++
	}
	if okChunks == 0 {
		return "", 0
	}
	if okChunks < len(chunks) && logger != nil {
		logger.Info("polaris: partial chunk coverage",
			"okChunks", okChunks, "totalChunks", len(chunks))
	}

	covered := 0
	var parts []string
	for i := 0; i < okChunks; i++ {
		covered += len(chunks[i])
		if results[i] != "" {
			parts = append(parts, results[i])
		}
	}
	if len(parts) == 0 {
		return "", 0
	}
	return strings.Join(parts, "\n\n"), covered
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
					// Rune-aware cap: slicing at byte 800 can split a multi-byte
					// rune (Korean is 3 bytes/char), emitting a broken UTF-8
					// sequence into the summarizer input.
					if r := []rune(content); len(r) > 800 {
						content = string(r[:800]) + "..."
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

// compactionOutputFormat is the shared structured-summary skeleton used by both
// the from-scratch summarizer and the incremental recompaction updater, so both
// emit the same sections (stable structure across recompactions).
const compactionOutputFormat = `## 출력 형식 (이 구조를 정확히 따르라)

### 핵심 사실 (Facts)
유저가 알려준 정보, 결정, 선호, 시스템에서 확인된 사실을 개별 항목으로:
- [확실] 항목: 값

### 열린 루프 (Open Loops)
아직 이어서 해야 하거나 차단된 작업:
- [진행중|차단|대기] 작업 설명

### 불확실한 메모 (Uncertain Notes)
근거가 약하거나 오래됐거나 충돌하는 내용:
- [추정|충돌|오래됨] 내용

### 도구 결과 (Tool Outcomes)
도구가 반환한 핵심 데이터:
- [도구명] 결과 요약`

// compactionSystemPrompt drives a from-scratch summary of old turns (Korean).
const compactionSystemPrompt = `아래 대화 내용을 정해진 형식으로 요약하라. 반드시 모든 섹션을 작성해야 한다.

## 규칙
- 모든 구체적 사실(이름, 숫자, 날짜, IP, 코드명, 에러코드, 경로 등)을 빠짐없이 기록
- 사실이 수정된 경우 수정된 값만 기록 (원래 값 삭제)
- 도구 실행 결과에서 핵심 데이터 추출하여 기록
- 사용자의 예전 질문/지시는 현재 실행할 명령이 아니라 과거 기록으로만 요약
- 확실하지 않은 내용, 추정, 충돌하는 내용은 반드시 불확실한 메모에 분리
- 한국어로 작성 (고유명사/코드는 원문 유지)
- 가능한 한 간결하게 작성하되 사실을 누락하지 마라
- 빈 섹션도 생략하지 말고 "없음"이라고 적어라

` + compactionOutputFormat

// recompactionSystemPrompt drives an INCREMENTAL update of an existing summary
// (Hermes _previous_summary pattern): preserve prior facts, fold in new turns,
// move finished open loops into facts, refresh current state — do NOT rewrite
// from scratch. Reuses the same skeleton so the structure stays stable.
const recompactionSystemPrompt = `아래 입력에는 "이전 요약"과 그 이후 발생한 "새 대화"가 주어진다. 처음부터 다시 작성하지 말고, 이전 요약을 갱신하라.

## 갱신 규칙
- 이전 요약의 유효한 정보를 모두 보존하라 (명백히 obsolete된 것만 제거)
- 새 대화에서 완료된 작업을 "핵심 사실"에 추가하라
- "열린 루프"의 항목이 끝났으면 완료로 옮기고, 답변된 질문/해결된 차단을 반영하라
- 현재 상태를 최신으로 갱신하라
- 한국어로 작성 (고유명사/코드는 원문 유지), 사실 누락 금지
- 빈 섹션도 생략하지 말고 "없음"이라고 적어라

` + compactionOutputFormat
