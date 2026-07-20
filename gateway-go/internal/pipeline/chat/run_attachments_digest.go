// run_attachments_digest.go — oversized chat-upload handling. A document's
// extracted text is injected verbatim up to an inline cap; past that, blind
// truncation would silently drop the tail of a 300-page PDF, so the overflow
// is split into chunks and digested by the LOCAL lightweight model (map
// stage) — the main model receives the verbatim head plus per-chunk digests.
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

// DigestOversizedDocumentText applies the oversized-document policy to text
// extracted outside the attachment pipeline — the miniapp document-capture
// path. Small text passes through unchanged.
func DigestOversizedDocumentText(ctx context.Context, name, text string) string {
	return digestOversizedDocument(ctx, name, text, docInlineRuneCap, defaultChunkSummarizer())
}

const digestSystemPrompt = "너는 문서 구간 요약 엔진이다. 주어진 문서 구간에서 사실만 추출해 한국어 불릿으로 압축한다. " +
	"수치·금액·날짜·기간·고유명사·조항 번호는 원문 그대로 보존한다. 해석·추측·서론 없이 내용만 출력한다."

// docChunk is one summarization unit with its rune range in the original text.
type docChunk struct {
	start, end int // rune offsets, [start, end)
	text       string
}

// splitRunes cuts rest (rune offsets relative to base) into chunks of at most
// chunkRunes, capped at maxChunks. Cutting at exact rune counts is fine for a
// digest — the lightweight model summarizes across a mid-sentence boundary
// without losing facts.
func splitRunes(rest []rune, base, chunkRunes, maxChunks int) []docChunk {
	var chunks []docChunk
	for off := 0; off < len(rest) && len(chunks) < maxChunks; off += chunkRunes {
		end := off + chunkRunes
		if end > len(rest) {
			end = len(rest)
		}
		chunks = append(chunks, docChunk{
			start: base + off,
			end:   base + end,
			text:  string(rest[off:end]),
		})
	}
	return chunks
}

// digestOversizedDocument returns text unchanged when it fits inlineCap.
// Oversized text is digested via summarize (verbatim head + chunk summaries);
// with no summarizer, or when every chunk fails, it degrades to a visible
// head truncation.
func digestOversizedDocument(ctx context.Context, name, text string, inlineCap int, summarize chunkSummarizer) string {
	runes := []rune(text)
	if len(runes) <= inlineCap {
		return text
	}
	if summarize == nil {
		return truncateWithNotice(runes, inlineCap, "경량 모델 미가용")
	}

	started := time.Now()
	head := string(runes[:digestHeadRunes])
	chunks := splitRunes(runes[digestHeadRunes:], digestHeadRunes, digestChunkRunes, digestMaxChunks)
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
		return truncateWithNotice(runes, inlineCap, "구간 요약 실패")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[대용량 문서 — 전체 %d자. 서두는 원문 그대로, 이후는 경량 모델의 구간별 요약(%d구간). "+
		"정확한 원문 인용이 필요하면 해당 구간을 특정해 사용자에게 원문 확인을 요청하라]\n\n", len(runes), len(chunks))
	fmt.Fprintf(&sb, "━━ 서두 원문 (1–%d자) ━━\n%s\n", digestHeadRunes, head)
	for k, c := range chunks {
		fmt.Fprintf(&sb, "\n━━ 구간 요약 %d/%d (%d–%d자) ━━\n", k+1, len(chunks), c.start+1, c.end)
		if summaries[k] == "" {
			sb.WriteString("[이 구간은 요약에 실패해 내용이 비어 있음]\n")
			continue
		}
		sb.WriteString(summaries[k] + "\n")
	}
	if covered := chunks[len(chunks)-1].end; covered < len(runes) {
		fmt.Fprintf(&sb, "\n[%d자 이후 잔여 구간은 요약 상한(%d구간) 초과로 생략]\n", covered, digestMaxChunks)
	}

	slog.Info("document digest complete",
		"doc", name, "runes", len(runes), "chunks", len(chunks),
		"failedChunks", len(chunks)-succeeded, "durationMs", time.Since(started).Milliseconds())
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
			user := fmt.Sprintf("문서 「%s」의 %d–%d자 구간:\n\n%s", name, c.start+1, c.end, c.text)
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
func truncateWithNotice(runes []rune, inlineCap int, reason string) string {
	return string(runes[:inlineCap]) +
		fmt.Sprintf("\n\n[문서가 길어 앞 %d자만 포함 — 전체 %d자 (%s)]", inlineCap, len(runes), reason)
}
