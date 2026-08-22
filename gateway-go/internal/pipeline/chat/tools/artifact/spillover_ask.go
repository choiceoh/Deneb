package artifact

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/pkg/promptguard"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Delegated-read bounds.
//
// The point of answering from a spilled blob is that the root model never sees
// the blob: paging it back 20K chars at a time is what re-rots the context we
// spilled to protect. So the map stage runs against the local model and only
// its answer — plus line numbers the root can verify with offset — comes back.
//
// The fan-out is deliberately small and sequential. Compaction bounds its own
// LLM digestion at 4 chunks per pass, and this borrows that ceiling rather than
// inventing a second cost model: a delegated read sits inside a user's turn, so
// its cost has to be bounded up front, not discovered.
const (
	spillAskMaxChunks     = 4
	spillAskChunkMaxChars = 12000
	spillAskChunkTokens   = 512
	spillAskReduceTokens  = 1024

	// spillAskCallTimeout bounds EACH delegated call, map and reduce alike. The tool inherits the
	// interactive turn context, whose backstop is 30 minutes, and the local-AI
	// hub path preserves the caller deadline instead of imposing its own — so
	// without this a single stalled helper request would eat the whole user
	// turn before the loop could try the next chunk or fall back to paging.
	spillAskCallTimeout = 90 * time.Second

	// spillAskLongLineHeadroom leaves room for the line number prefix and the
	// truncation notice when a single oversized line is clipped.
	spillAskLongLineHeadroom = 128
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

	// Injection-bearing blobs must not be delegated. The tool-result fence
	// (agent.fenceUntrustedToolOutput) scans what a tool RETURNS: paging
	// returns the raw bytes and gets scanned, but a delegated answer returns
	// only a paraphrase — which would launder a hidden instruction sitting in
	// the dropped middle into clean-looking, in-session evidence. When the
	// blob trips promptguard we refuse to delegate and let the caller page,
	// so the existing chokepoint sees the real bytes.
	//
	// Scan the RAW blob, never the chunks: chunk text prefixes every line with
	// "N: ", which would stop line-anchored signatures (role impersonation) from
	// matching content that paging would have caught.
	if len(promptguard.Scan(strings.Join(lines, "\n"))) > 0 {
		return "", false
	}

	type partial struct {
		firstLine, lastLine int
		clippedLines        int
		answer              string
	}
	var partials []partial
	for _, c := range chunks {
		user := fmt.Sprintf("## 질문\n%s\n\n## 발췌 (%d–%d줄)\n%s",
			question, c.firstLine, c.lastLine, c.text)
		callCtx, cancel := context.WithTimeout(ctx, spillAskCallTimeout)
		answer, err := ask(callCtx, spillAskSystemPrompt, user, spillAskChunkTokens)
		cancel()
		if err != nil || strings.TrimSpace(answer) == "" {
			continue // one chunk failing must not sink the whole read
		}
		// The citation contract is only a prompt instruction, and a local model
		// is free to ignore it. An answer with no in-range [L<n>] is not
		// verifiable from the root's seat, which is the whole basis for
		// returning an answer instead of the text — so drop it exactly like a
		// failed chunk rather than presenting it as grounded evidence.
		if !hasInRangeCitation(answer, c.firstLine, c.lastLine) && !isNoEvidenceAnswer(answer) {
			continue
		}
		partials = append(partials, partial{
			firstLine: c.firstLine, lastLine: c.lastLine,
			clippedLines: c.clippedLines, answer: strings.TrimSpace(answer),
		})
	}
	if len(partials) == 0 {
		return "", false
	}

	// Coverage is stated up front, not buried: the model must know it is
	// reading an answer drawn from part of the blob, not all of it. It counts
	// only the chunks that actually ANSWERED — counting attempted chunks would
	// claim a failed region was searched, which is the exact coverage-hiding
	// defect this change fixes for polaris expand.
	scanned, clipped := 0, 0
	for _, p := range partials {
		scanned += p.lastLine - p.firstLine + 1
		clipped += p.clippedLines
	}
	var head strings.Builder
	fmt.Fprintf(&head, "## %s 위임 답변\n\n**질문:** %s\n", spillID, question)
	fmt.Fprintf(&head, "**근거 범위:** 총 %d줄 중 %d줄 스캔(%d구간)", len(lines), scanned, len(partials))
	// A clipped line is counted as scanned because its number was covered, but
	// most of its bytes never reached the delegate — minified JSON or an
	// encoded blob is one line and would otherwise read as "fully searched".
	if clipped > 0 {
		fmt.Fprintf(&head, " · 그중 %d줄은 너무 길어 앞부분만 전달됨", clipped)
	}
	if scanned < len(lines) || clipped > 0 {
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
	reduceCtx, cancelReduce := context.WithTimeout(ctx, spillAskCallTimeout)
	reduced, err := ask(reduceCtx, spillAskReduceSystemPrompt,
		fmt.Sprintf("## 질문\n%s\n\n## 부분 답변\n%s", question, merged.String()), spillAskReduceTokens)
	cancelReduce()
	// A reduction that drops every citation is no longer verifiable either, so
	// it is treated like a failed reduce and the cited partials are returned.
	if err != nil || strings.TrimSpace(reduced) == "" || !hasAnyCitation(reduced) {
		// Reduce failed — the partials are still real evidence, so return them
		// rather than dropping work the local model already did.
		return head.String() + strings.TrimRight(merged.String(), "\n") + spillAskVerifyHint(spillID), true
	}
	return head.String() + strings.TrimSpace(reduced) + spillAskVerifyHint(spillID), true
}

// citationRe matches the [L<number>] citations the delegate is told to emit.
var citationRe = regexp.MustCompile(`\[L(\d+)\]`)

// hasInRangeCitation reports whether the answer cites at least one line inside
// the chunk it was shown. An out-of-range number is not a usable pointer: the
// root would open a line the delegate never read.
func hasInRangeCitation(answer string, firstLine, lastLine int) bool {
	for _, m := range citationRe.FindAllStringSubmatch(answer, -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= firstLine && n <= lastLine {
			return true
		}
	}
	return false
}

// hasAnyCitation reports whether the merged answer kept any citation at all.
func hasAnyCitation(answer string) bool {
	return citationRe.MatchString(answer)
}

// isNoEvidenceAnswer recognizes the delegate's explicit "nothing here" reply,
// which is a legitimate uncited outcome the system prompt asks for.
func isNoEvidenceAnswer(answer string) bool {
	return strings.Contains(answer, "근거 없음")
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
	clippedLines        int // lines too long to send whole; only their head went
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
	clipped := 0

	flush := func(last int) {
		if b.Len() == 0 {
			return
		}
		chunks = append(chunks, spillAskChunk{
			firstLine: first, lastLine: last, text: b.String(), clippedLines: clipped,
		})
		b.Reset()
		clipped = 0
	}

	for i, line := range lines {
		// A single line can exceed the whole chunk budget — minified JSON, a
		// base64 payload, compact command output. Without this the "bounded"
		// request would carry the entire line and blow the helper's context.
		lineClipped := false
		if len(line) > spillAskChunkMaxChars {
			line = textutil.TruncateBytes(line, spillAskChunkMaxChars-spillAskLongLineHeadroom) +
				" …[이 줄은 너무 길어 앞부분만 전달됨 (원문 " + fmt.Sprint(i+1) + "번째 줄)]"
			lineClipped = true
		}
		entry := fmt.Sprintf("%d: %s\n", i+1, line)
		if b.Len() > 0 && b.Len()+len(entry) > spillAskChunkMaxChars {
			flush(i) // previous line closed this chunk
			if len(chunks) >= spillAskMaxChunks {
				return chunks
			}
			first = i + 1
		}
		b.WriteString(entry)
		// Counted AFTER the flush decision, so the clip lands on the chunk that
		// actually carries the line. Counting before it would credit the
		// previous chunk and leave this one reporting a clean full scan — the
		// coverage-hiding case this whole path exists to prevent.
		if lineClipped {
			clipped++
		}
	}
	flush(len(lines))
	return chunks
}
