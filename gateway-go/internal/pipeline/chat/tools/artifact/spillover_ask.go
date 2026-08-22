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
)

// Timeouts are vars, not consts, purely so tests can shrink them: the
// behaviour they encode (a stalled helper must reach the paging fallback
// quickly) is otherwise only observable by waiting minutes. Nothing at runtime
// writes them.
var (
	// spillAskCallTimeout bounds EACH delegated call, map and reduce alike. The
	// tool inherits the interactive turn context, whose backstop is 30 minutes,
	// and the local-AI hub path preserves the caller deadline instead of
	// imposing its own — so without this a single stalled helper request would
	// eat the whole user turn before the loop could try the next chunk or fall
	// back to paging.
	spillAskCallTimeout = 90 * time.Second

	// spillAskPhaseTimeout bounds the delegated read AS A WHOLE. Per-call
	// timeouts alone do not: the map stage is sequential, so four stalled
	// chunks each burn their own 90s and the reduce adds a fifth — roughly
	// seven and a half minutes before the caller reaches the paging fallback,
	// inside one interactive turn on a phone. Every call derives from this
	// budget, so repeated helper failures degrade to paging promptly and the
	// partial answers already collected are still returned.
	spillAskPhaseTimeout = 150 * time.Second
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
func spillAsk(ctx context.Context, ask tooldeps.LocalAIFunc, spillID, content string, lines []string, question string) (string, bool) {
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
	//
	// The caller's already-loaded content, not strings.Join(lines, "\n"): the
	// join allocated a second full-size copy of a blob that can run to the
	// session ceiling, doubling peak memory on one interactive read to rebuild
	// bytes we were handed.
	if len(promptguard.Scan(content)) > 0 {
		return "", false
	}

	// One budget for the whole phase; each call below derives from it, so the
	// worst case is this deadline rather than the sum of the per-call ones.
	phaseCtx, cancelPhase := context.WithTimeout(ctx, spillAskPhaseTimeout)
	defer cancelPhase()

	// One delegated read at a time. read_spillover is classified parallel-safe,
	// so a single assistant turn can emit several question-bearing calls and the
	// executor runs them concurrently — each then launching its own four map
	// requests plus a reduce as critical local-hub work. The four-chunk bound is
	// per invocation, so the turn's real fan-out would be however many calls the
	// model happened to make.
	//
	// Serialize rather than widen: the local hub is one box, and a queued read
	// that answers beats two that contend and time out. A call that spends its
	// whole budget waiting falls back to paging, which is the honest
	// degradation — the caller says so rather than returning page 1 as an answer.
	select {
	case spillAskSlots <- struct{}{}:
		defer func() { <-spillAskSlots }()
	case <-phaseCtx.Done():
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
		callCtx, cancel := context.WithTimeout(phaseCtx, spillAskCallTimeout)
		answer, err := ask(callCtx, spillAskSystemPrompt, user, spillAskChunkTokens)
		// Checked BEFORE cancel(), which would set Err() itself. The local-AI
		// hub returns whatever text it accumulated with a nil error when the
		// stream is cut mid-sentence, so a deadline that fired is the only
		// signal that this answer is a fragment — and a fragment that happens
		// to carry a citation would otherwise be presented as a grounded,
		// complete answer.
		timedOut := callCtx.Err() != nil
		cancel()
		if err != nil || timedOut || strings.TrimSpace(answer) == "" {
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
	ranges := make([][2]int, 0, len(partials))
	for _, p := range partials {
		ranges = append(ranges, [2]int{p.firstLine, p.lastLine})
	}
	reduceCtx, cancelReduce := context.WithTimeout(phaseCtx, spillAskCallTimeout)
	reduced, err := ask(reduceCtx, spillAskReduceSystemPrompt,
		fmt.Sprintf("## 질문\n%s\n\n## 부분 답변\n%s", question, merged.String()), spillAskReduceTokens)
	reduceTimedOut := reduceCtx.Err() != nil
	cancelReduce()
	// A reduction that drops every citation — or invents one — is no longer
	// verifiable, so it is treated like a failed reduce and the cited partials
	// are returned instead.
	if err != nil || reduceTimedOut || strings.TrimSpace(reduced) == "" || !citationsAllInRanges(reduced, ranges) {
		// Reduce failed — the partials are still real evidence, so return them
		// rather than dropping work the local model already did.
		return head.String() + strings.TrimRight(merged.String(), "\n") + spillAskVerifyHint(spillID), true
	}
	return head.String() + strings.TrimSpace(reduced) + spillAskVerifyHint(spillID), true
}

// spillAskSlots admits one delegated read at a time; see spillAsk.
var spillAskSlots = make(chan struct{}, 1)

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

// citationsAllInRanges reports whether the merged answer carries at least one
// citation and EVERY citation it carries falls inside a range the delegate
// actually read.
//
// Requiring merely "a citation" let the reducer mint one: [L999999] matches the
// syntax, so a fabricated pointer passed as verification while pointing at a
// line no chunk was ever shown. The root's only check on a delegated answer is
// re-opening a cited line, so a citation outside every successful chunk is
// worse than none — it looks verifiable and is not. Falling back to the cited
// partials keeps the real evidence.
func citationsAllInRanges(answer string, ranges [][2]int) bool {
	matches := citationRe.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return false
	}
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return false
		}
		inSome := false
		for _, r := range ranges {
			if n >= r[0] && n <= r[1] {
				inSome = true
				break
			}
		}
		if !inSome {
			return false
		}
	}
	return true
}

// noEvidenceSentence is the reply the system prompt asks for verbatim;
// noEvidenceShort is the bare form small local models tend to emit instead.
const (
	noEvidenceSentence = "이 구간에는 근거 없음"
	noEvidenceShort    = "근거 없음"
)

// isNoEvidenceAnswer recognizes the delegate's explicit "nothing here" reply,
// which is a legitimate uncited outcome the system prompt asks for.
//
// It compares against the expected sentence rather than looking for it. A
// length bound plus strings.Contains was still a bypass: "이 구간에는 근거 없음.
// 정답은 42." sits under any reasonable length cap while smuggling an uncited
// claim past the citation gate — which is the one check standing between the
// root and an unverifiable answer.
func isNoEvidenceAnswer(answer string) bool {
	trimmed := strings.Trim(strings.TrimSpace(answer), "\"'“”‘’`*[]()")
	// Only trailing sentence punctuation is forgiven; anything after it is
	// another claim, not decoration.
	trimmed = strings.TrimRight(trimmed, " .。!?~…\t")
	return trimmed == noEvidenceSentence || trimmed == noEvidenceShort
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
			// Size the cut from the ACTUAL notice — it is Korean, so a fixed
			// byte headroom would overshoot the chunk budget.
			notice := " …[이 줄은 너무 길어 앞부분만 전달됨 (원문 " + fmt.Sprint(i+1) + "번째 줄)]"
			// The line is emitted as "N: <line>\n", so the prefix and the
			// newline come out of the same budget — otherwise the entry that
			// carries this line lands just over it.
			keep := spillAskChunkMaxChars - len(notice) - len(fmt.Sprintf("%d: \n", i+1))
			if keep < 0 {
				keep = 0
			}
			line = textutil.TruncateBytes(line, keep) + notice
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
