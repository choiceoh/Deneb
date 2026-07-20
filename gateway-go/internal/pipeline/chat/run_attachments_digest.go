// run_attachments_digest.go — oversized chat-upload handling. A document's
// extracted text is injected verbatim up to an inline cap; past that, blind
// truncation would silently drop the tail of a 300-page PDF, so the overflow
// is split into line-ranged chunks and digested by the LOCAL lightweight
// model (map stage). The main model receives a MAP: a table of contents
// (chunk → file line range → topic), the verbatim head, and per-chunk
// digests — and, when the original was archived to a capture file, it can
// open any mapped range directly with the read tool to verify exact wording.
// When local AI is unavailable the fallback is a visible head truncation —
// never a silent drop.
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

const (
	// docInlineRuneCap keeps real workloads inline: a project IM of ~100K
	// runes is consumed whole to good effect. Only genuinely huge documents
	// go through the digest.
	docInlineRuneCap = 120_000
	// turnInlineRuneCap bounds the combined injected document text of one
	// turn; once spent, later documents get a smaller per-doc allowance
	// (floored at docInlineRuneFloor so nothing degrades to uselessness).
	turnInlineRuneCap  = 160_000
	docInlineRuneFloor = 20_000

	// Digest shape: verbatim head, then lightweight-model chunk summaries.
	digestHeadRunes   = 6_000
	digestChunkRunes  = 20_000
	digestMaxChunks   = 30
	digestChunkTokens = 700
	// digestConcurrency bounds parallel lightweight calls — the local vLLM
	// batches them, and 3 keeps the sidecar responsive for other work.
	digestConcurrency = 3
)

// chunkSummarizer produces a bounded summary for one document chunk. nil means
// no summarizer is available (local AI down or gated off) — callers then fall
// back to visible truncation.
type chunkSummarizer func(ctx context.Context, system, user string, maxTokens int) (string, error)

// defaultChunkSummarizer returns the lightweight-model summarizer, or nil when
// local AI is unavailable (model-roles.md: summarization helpers stay on the
// local lightweight role, never a cloud role).
func defaultChunkSummarizer() chunkSummarizer {
	if pilot.LocalAIHub() == nil {
		return nil
	}
	return func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		return pilot.CallLocalLLM(ctx, system, user, maxTokens)
	}
}

// digestSource points at the archived original of a digested document.
type digestSource struct {
	// path is an agent-openable path (absolute; the read tool allows the
	// memory root). "" = original not archived.
	path string
	// bodyLine is the 1-based file line where the document body starts
	// (capture files carry a metadata header above the body).
	bodyLine int
}

// fileLine maps a 1-based document line to the archived file's line number.
func (s digestSource) fileLine(docLine int) int {
	if s.path == "" || s.bodyLine <= 0 {
		return docLine
	}
	return docLine + s.bodyLine - 1
}

// DigestOversizedDocumentText applies the oversized-document policy to text
// extracted outside the attachment pipeline — the miniapp document-capture
// path. sourcePath/sourceBodyLine locate the already-archived original ("",
// 0 when unavailable). Small text passes through unchanged (normalized).
func DigestOversizedDocumentText(ctx context.Context, name, text, sourcePath string, sourceBodyLine int) string {
	return digestOversizedDocument(ctx, name, text, docInlineRuneCap, defaultChunkSummarizer(),
		digestSource{path: sourcePath, bodyLine: sourceBodyLine})
}

const digestSystemPrompt = "너는 문서 구간 요약 엔진이다. 첫 줄은 반드시 '주제: <구간 내용 한 줄>' 형식으로 쓴다. " +
	"그다음 줄부터 구간의 사실만 한국어 불릿으로 압축한다. 수치·금액·날짜·기간·고유명사·조항 번호는 " +
	"원문 그대로 보존한다. 해석·추측·서론 없이 내용만 출력한다."

// docChunk is one summarization unit with its 1-based inclusive line range in
// the normalized document text.
type docChunk struct {
	startLine, endLine int
	text               string
}

// splitLineChunks groups lines[firstIdx:] into chunks of about chunkRunes
// runes each (never splitting a line), capped at maxChunks. Line numbers are
// 1-based over the full lines slice.
func splitLineChunks(lines []string, firstIdx, chunkRunes, maxChunks int) []docChunk {
	var chunks []docChunk
	i := firstIdx
	for i < len(lines) && len(chunks) < maxChunks {
		start := i
		runes := 0
		var sb strings.Builder
		for i < len(lines) {
			lineRunes := utf8.RuneCountInString(lines[i])
			if runes > 0 && runes+lineRunes > chunkRunes {
				break
			}
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(lines[i])
			runes += lineRunes
			i++
		}
		chunks = append(chunks, docChunk{startLine: start + 1, endLine: i, text: sb.String()})
	}
	return chunks
}

// headLineCount returns how many leading lines make up the verbatim head
// (~digestHeadRunes runes, at least one line).
func headLineCount(lines []string) int {
	runes, n := 0, 0
	for _, ln := range lines {
		if n > 0 && runes+utf8.RuneCountInString(ln) > digestHeadRunes {
			break
		}
		runes += utf8.RuneCountInString(ln)
		n++
	}
	if n == 0 {
		n = 1
	}
	return n
}

// chunkTopic extracts the one-line topic from a chunk summary ("주제: …" per
// the prompt), falling back to the summary's first line.
func chunkTopic(summary string) string {
	first := summary
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	first = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), "주제:"))
	if r := []rune(first); len(r) > 60 {
		first = string(r[:60]) + "…"
	}
	return first
}

// digestOversizedDocument returns text (normalized) unchanged when it fits
// inlineCap. Oversized text becomes a map — TOC, verbatim head, per-chunk
// digests with source line ranges; with no summarizer, or when every chunk
// fails, it degrades to a visible head truncation. Normalization matches
// wiki.SaveCaptureAt exactly so mapped line numbers stay aligned with the
// archived file.
func digestOversizedDocument(ctx context.Context, name, text string, inlineCap int, summarize chunkSummarizer, src digestSource) string {
	text = wiki.NormalizeCaptureText(text)
	runes := []rune(text)
	if len(runes) <= inlineCap {
		return text
	}
	if summarize == nil {
		return truncateWithNotice(runes, inlineCap, "경량 모델 미가용", src)
	}

	started := time.Now()
	lines := strings.Split(text, "\n")
	headN := headLineCount(lines)
	head := strings.Join(lines[:headN], "\n")
	chunks := splitLineChunks(lines, headN, digestChunkRunes, digestMaxChunks)
	summaries := digestChunks(ctx, name, chunks, summarize)

	succeeded := 0
	for _, s := range summaries {
		if s != "" {
			succeeded++
		}
	}
	if succeeded == 0 {
		slog.Warn("document digest failed entirely; falling back to head truncation",
			"doc", name, "runes", len(runes), "chunks", len(chunks))
		return truncateWithNotice(runes, inlineCap, "구간 요약 실패", src)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[대용량 문서 지도 — 「%s」 전체 %d자·%d줄. 서두는 원문 그대로, 구간 요약은 경량 모델 산출]\n",
		name, len(runes), len(lines))
	if src.path != "" {
		fmt.Fprintf(&sb, "원문 전체 보관: %s — 아래 줄번호는 이 파일 기준이다. 수치·문구를 정확히 확인할 땐 "+
			"요약을 믿지 말고 read 도구(offset=시작줄, limit=줄수)로 해당 구간 원문을 직접 열람하라.\n", src.path)
	} else {
		sb.WriteString("원문 미보관 — 정확한 인용이 필요하면 구간을 특정해 사용자에게 원문 확인을 요청하라.\n")
	}

	sb.WriteString("\n━━ 목차 ━━\n")
	fmt.Fprintf(&sb, "서두 원문: %d–%d줄\n", src.fileLine(1), src.fileLine(headN))
	for k, c := range chunks {
		topic := "(요약 실패)"
		if summaries[k] != "" {
			topic = chunkTopic(summaries[k])
		}
		fmt.Fprintf(&sb, "구간 %d: %d–%d줄 — %s\n", k+1, src.fileLine(c.startLine), src.fileLine(c.endLine), topic)
	}

	fmt.Fprintf(&sb, "\n━━ 서두 원문 (%d–%d줄) ━━\n%s\n", src.fileLine(1), src.fileLine(headN), head)
	for k, c := range chunks {
		fmt.Fprintf(&sb, "\n━━ 구간 %d/%d 요약 (%d–%d줄) ━━\n", k+1, len(chunks), src.fileLine(c.startLine), src.fileLine(c.endLine))
		if summaries[k] == "" {
			sb.WriteString("[이 구간은 요약에 실패해 내용이 비어 있음 — 원문 줄 범위를 직접 열람하라]\n")
			continue
		}
		sb.WriteString(summaries[k] + "\n")
	}
	if covered := chunks[len(chunks)-1].endLine; covered < len(lines) {
		fmt.Fprintf(&sb, "\n[%d줄 이후 잔여 구간은 요약 상한(%d구간) 초과로 미요약 — 필요 시 원문 파일에서 직접 열람]\n",
			src.fileLine(covered), digestMaxChunks)
	}

	slog.Info("document digest complete",
		"doc", name, "runes", len(runes), "lines", len(lines), "chunks", len(chunks),
		"failedChunks", len(chunks)-succeeded, "archived", src.path != "",
		"durationMs", time.Since(started).Milliseconds())
	return sb.String()
}

// digestChunks summarizes chunks with bounded concurrency. Each result lands
// in its own slot, so output order matches input order; a failed chunk leaves
// its slot empty and is surfaced by the caller.
func digestChunks(ctx context.Context, name string, chunks []docChunk, summarize chunkSummarizer) []string {
	summaries := make([]string, len(chunks))
	var wg sync.WaitGroup
	sem := make(chan struct{}, digestConcurrency)
	for k, c := range chunks {
		acquired := false
		select {
		case sem <- struct{}{}:
			acquired = true
		case <-ctx.Done():
		}
		if !acquired {
			break // extraction budget spent — remaining slots stay empty
		}
		wg.Add(1)
		go func(k int, c docChunk) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() { _ = recover() }() // one chunk's panic must not crash the gateway
			user := fmt.Sprintf("문서 「%s」의 %d–%d줄 구간:\n\n%s", name, c.startLine, c.endLine, c.text)
			out, err := summarize(ctx, digestSystemPrompt, user, digestChunkTokens)
			if err != nil {
				slog.Warn("document chunk digest failed", "doc", name, "chunk", k+1, "error", err)
				return
			}
			summaries[k] = strings.TrimSpace(out)
		}(k, c)
	}
	wg.Wait()
	return summaries
}

// truncateWithNotice keeps the head of an oversized document and makes the
// cut visible to the model — a silent drop would read as "covered everything".
// When the original was archived, the notice points at it so the tail stays
// reachable.
func truncateWithNotice(runes []rune, inlineCap int, reason string, src digestSource) string {
	notice := fmt.Sprintf("\n\n[문서가 길어 앞 %d자만 포함 — 전체 %d자 (%s)]", inlineCap, len(runes), reason)
	if src.path != "" {
		notice = fmt.Sprintf("\n\n[문서가 길어 앞 %d자만 포함 — 전체 %d자 (%s). 원문 전체 보관: %s (본문은 %d줄부터) — read 도구로 직접 열람 가능]",
			inlineCap, len(runes), reason, src.path, src.bodyLine)
	}
	return string(runes[:inlineCap]) + notice
}
