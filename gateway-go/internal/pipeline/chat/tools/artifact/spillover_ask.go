package artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// Delegated-read bounds.
//
// The point of answering from a spilled blob is that the root model never sees
// the blob: paging it back 20K chars at a time is what re-rots the context we
// spilled to protect. So the map stage runs against the local model and only
// its answer — plus line numbers the root can verify with offset — comes back.
//
// The fan-out is deliberately small and sequential. Compaction bounds its own
// LLM digestion at 4 chunks per pass for the same reason (an interactive turn
// lives inside a 5-minute deadline), and this borrows that ceiling rather than
// inventing a second cost model.
const (
	spillAskMaxChunks     = 4
	spillAskChunkMaxChars = 12000
	spillAskChunkTokens   = 512
	spillAskReduceTokens  = 1024
)

const spillAskSystemPrompt = "아래는 큰 도구 출력의 일부다. 질문에 이 발췌만 근거로 답하라. " +
	"근거가 된 줄 번호를 반드시 [L<번호>] 형태로 인용하고, 발췌에 근거가 없으면 " +
	"추측하지 말고 '이 구간에는 근거 없음'이라고만 답하라. 한국어로 답변."

const spillAskReduceSystemPrompt = "같은 문서의 여러 구간에 대한 부분 답변들이다. " +
	"이를 하나의 답으로 합쳐라. 상충하면 그 사실을 밝히고, 줄 번호 인용([L<번호>])은 " +
	"그대로 보존하라. 근거 없는 구간의 답변은 버려라. 한국어로 답변."

// spillAsk answers question from the blob by delegating bounded chunks to the
// local model and merging the partial answers. ok=false means the delegation
// produced nothing usable and the caller should fall back to paging.
//
// Chunking is by line so the cited [L<n>] numbers stay meaningful: they are the
// blob's own 1-based line numbers, which the root model can re-open directly
// with read_spillover(offset=n). An answer the root cannot verify against the
// source is worse than a page it can read itself.
func spillAsk(ctx context.Context, ask tooldeps.LocalAIFunc, spillID string, lines []string, question string) (string, bool) {
	chunks := spillAskChunks(lines)
	if len(chunks) == 0 {
		return "", false
	}

	type partial struct {
		firstLine, lastLine int
		answer              string
	}
	var partials []partial
	for _, c := range chunks {
		user := fmt.Sprintf("## 질문\n%s\n\n## 발췌 (%d–%d줄)\n%s",
			question, c.firstLine, c.lastLine, c.text)
		answer, err := ask(ctx, spillAskSystemPrompt, user, spillAskChunkTokens)
		if err != nil || strings.TrimSpace(answer) == "" {
			continue // one chunk failing must not sink the whole read
		}
		partials = append(partials, partial{firstLine: c.firstLine, lastLine: c.lastLine, answer: strings.TrimSpace(answer)})
	}
	if len(partials) == 0 {
		return "", false
	}

	// Coverage is stated up front, not buried: the model must know it is
	// reading an answer drawn from part of the blob, not all of it. The
	// scanned-lines count is what the chunks actually covered.
	scanned := 0
	for _, c := range chunks {
		scanned += c.lastLine - c.firstLine + 1
	}
	var head strings.Builder
	fmt.Fprintf(&head, "## %s 위임 답변\n\n**질문:** %s\n", spillID, question)
	fmt.Fprintf(&head, "**근거 범위:** 총 %d줄 중 %d줄 스캔(%d구간)", len(lines), scanned, len(chunks))
	if scanned < len(lines) {
		fmt.Fprintf(&head, " — 전체를 다 보지는 않았습니다. 빠진 구간이 걸리면 grep=\"패턴\"으로 좁히세요")
	}
	head.WriteString("\n\n")

	if len(partials) == 1 {
		return head.String() + partials[0].answer + spillAskVerifyHint(spillID), true
	}

	var merged strings.Builder
	for _, p := range partials {
		fmt.Fprintf(&merged, "### %d–%d줄\n%s\n\n", p.firstLine, p.lastLine, p.answer)
	}
	reduced, err := ask(ctx, spillAskReduceSystemPrompt,
		fmt.Sprintf("## 질문\n%s\n\n## 부분 답변\n%s", question, merged.String()), spillAskReduceTokens)
	if err != nil || strings.TrimSpace(reduced) == "" {
		// Reduce failed — the partials are still real evidence, so return them
		// rather than dropping work the local model already did.
		return head.String() + strings.TrimRight(merged.String(), "\n") + spillAskVerifyHint(spillID), true
	}
	return head.String() + strings.TrimSpace(reduced) + spillAskVerifyHint(spillID), true
}

// spillAskVerifyHint tells the root how to check a cited line itself. Without
// it a delegated answer is unfalsifiable from the root's seat.
func spillAskVerifyHint(spillID string) string {
	return fmt.Sprintf("\n\n_인용 확인: read_spillover(spill_id=%q, offset=<줄번호>)_", spillID)
}

// spillAskChunk is one contiguous line window handed to the local model.
type spillAskChunk struct {
	firstLine, lastLine int // 1-based, inclusive
	text                string
}

// spillAskChunks splits lines into at most spillAskMaxChunks windows bounded by
// spillAskChunkMaxChars, each line prefixed with its 1-based number so the
// model can cite positions that survive back in the root context.
//
// A blob larger than the fan-out can hold is covered head-first rather than
// sampled: a truthful partial scan the caller is told about beats a scattered
// one it cannot reason about. The stated coverage above makes the gap visible.
func spillAskChunks(lines []string) []spillAskChunk {
	var chunks []spillAskChunk
	var b strings.Builder
	first := 1

	flush := func(last int) {
		if b.Len() == 0 {
			return
		}
		chunks = append(chunks, spillAskChunk{firstLine: first, lastLine: last, text: b.String()})
		b.Reset()
	}

	for i, line := range lines {
		entry := fmt.Sprintf("%d: %s\n", i+1, line)
		if b.Len() > 0 && b.Len()+len(entry) > spillAskChunkMaxChars {
			flush(i) // previous line closed this chunk
			if len(chunks) >= spillAskMaxChunks {
				return chunks
			}
			first = i + 1
		}
		b.WriteString(entry)
	}
	flush(len(lines))
	return chunks
}
