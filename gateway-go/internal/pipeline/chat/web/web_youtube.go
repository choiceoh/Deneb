// web_youtube.go — YouTube transcript summarization.
//
// YouTube transcripts run to tens of thousands of characters. Dropping the full
// transcript into the conversation transcript is exactly what makes "summarize a
// few YouTube links" overflow the protected context window: each result lands in
// the recent (non-compactable) turns, and a handful of them exceed the budget
// before compaction can touch anything (see docs/agent-rules/prompt-cache.md §5 +
// chat/compact_guard.go protectedZoneExceedsBudget).
//
// To keep the main context small, we summarize the transcript in an isolated
// local-LLM call (pilot.CallLocalLLM, which carries its own model fallback
// chain) and return only the summary. The full transcript is preserved on disk
// via the spillover store, so the agent can still pull exact quotes later with
// read_spillover. When the local summarizer is unavailable, we fall back to a
// bounded excerpt — never the full transcript — so a batch of links can never
// reintroduce the overflow.
package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

const (
	// youtubeSummarizeMinChars — transcripts shorter than this stay inline;
	// summarizing tiny captions adds latency without meaningfully saving context.
	youtubeSummarizeMinChars = 2000
	// youtubeSummaryChunkChars is one summarizer call's transcript slice. A
	// single capped call proved the detail ceiling (operator 2026-07-18: a 1h
	// video lost half its transcript to the old 32k input cap) — long
	// transcripts now split into chunks summarized in parallel, so detail
	// scales with video length instead of truncating.
	youtubeSummaryChunkChars = 24000
	// youtubeSummaryMaxChunks bounds cost/latency (4×24k ≈ a 2-3h talk). A
	// longer tail is dropped with an explicit scope note; the full transcript
	// is always in spillover.
	youtubeSummaryMaxChunks = 4
	// youtubeSummaryMaxTokens is the per-call output ceiling. The models stop
	// naturally well under it on a 24k-char slice; this is headroom for dense
	// segments, not a padding target.
	youtubeSummaryMaxTokens = 8000
	// youtubeConclusionMaxTokens bounds the cross-chunk 핵심 결론 pass.
	youtubeConclusionMaxTokens = 700
	// youtubeFallbackExcerptChars bounds the inline excerpt kept when the local
	// summarizer is unavailable.
	youtubeFallbackExcerptChars = 6000
	// youtubeSummaryTimeout is the per-call deadline (chunks run in parallel,
	// so it also approximates the whole pipeline). Local prose decode runs
	// ~28-42 tok/s; a short deadline would truncate into the fallback-excerpt
	// path and silently discard the detail.
	youtubeSummaryTimeout = 180 * time.Second
)

// youtubeSummaryDetailCore is the shared detail contract for both the
// single-call and per-chunk prompts: exhaustive, section-structured, fact-preserving.
const youtubeSummaryDetailCore = "당신은 유튜브 영상 자막을 한국어로 아주 상세하게 요약하는 전문가입니다. " +
	"분량을 아끼지 말고, 자막에 담긴 논점을 빠짐없이 다루세요. " +
	"전개 순서를 따라 내용을 섹션별 소제목으로 나누고, " +
	"각 섹션마다 논점·근거·예시·설명 과정을 구체적으로 풀어 쓰세요. " +
	"중요한 수치·이름·날짜·고유명사·인용은 빠짐없이 보존하고, " +
	"화자의 주장과 객관적 사실 전달은 구분해서 서술하세요. " +
	"불필요한 서두 없이 요약 내용만 바로 출력하세요."

const youtubeSummarySystemPrompt = youtubeSummaryDetailCore +
	" 마지막에 '핵심 결론' 섹션으로 전체를 3–5줄로 압축하세요. " +
	"전사 맨 앞에 '[참고: …구간만 다룹니다]' 같은 범위 안내가 있으면, " +
	"요약이 영상 전체가 아님을 서두에 한 줄로 밝히세요."

// youtubeChunkSystemPrompt summarizes ONE slice of a long transcript; the
// cross-chunk 핵심 결론 is added by a separate pass, so chunks must not write one.
const youtubeChunkSystemPrompt = youtubeSummaryDetailCore +
	" 지금 주어지는 자막은 긴 영상의 한 구간입니다. 이 구간의 내용만 다루고, " +
	"영상 전체에 대한 결론이나 총평은 쓰지 마세요."

const youtubeConclusionSystemPrompt = "다음은 한 유튜브 영상의 구간별 상세 요약입니다. " +
	"전체를 아우르는 '핵심 결론'을 3–6줄로 작성하세요. 결론 본문만 출력하세요."

// summarizeYouTubeResult turns a raw YouTube extraction into a compact result
// for the conversation transcript: metadata + a generated summary, with the
// full transcript offloaded to spillover. Short or transcript-less results pass
// through unchanged.
func summarizeYouTubeResult(ctx context.Context, spill tooldeps.SpilloverStore, r *media.YouTubeResult) string {
	if !r.HasTranscript() || utf8.RuneCountInString(r.Transcript) < youtubeSummarizeMinChars {
		return media.FormatYouTubeResult(r)
	}

	// Preserve the full transcript on disk first so exact quotes remain
	// retrievable regardless of the summarization outcome.
	spillID := storeYouTubeTranscript(ctx, spill, r)

	summary, err := summarizeTranscript(ctx, r)
	if err != nil || strings.TrimSpace(summary) == "" {
		return formatYouTubeFallback(r, spillID)
	}
	return formatYouTubeSummary(r, summary, spillID)
}

// summarizeTranscript produces the detailed summary: one call for short
// transcripts, parallel per-chunk calls + a conclusion pass for long ones.
func summarizeTranscript(ctx context.Context, r *media.YouTubeResult) (string, error) {
	// Skip the call when the local AI was recently confirmed down — avoids N
	// sequential timeouts for a batch of links.
	if pilot.LocalAIRecentlyDown() {
		return "", fmt.Errorf("local summarizer unavailable")
	}
	runes := []rune(r.Transcript)
	// 25% slack so a transcript barely over one chunk doesn't split into a
	// full chunk plus a stub.
	if len(runes) <= youtubeSummaryChunkChars+youtubeSummaryChunkChars/4 {
		prompt := fmt.Sprintf("제목: %s\n채널: %s\n\n자막:\n%s", r.Title, r.Channel, string(runes))
		return callYoutubeSummarizer(ctx, youtubeSummarySystemPrompt, prompt, youtubeSummaryMaxTokens)
	}
	return summarizeTranscriptChunked(ctx, r, runes)
}

// callYoutubeSummarizer is one bounded lightweight-model call. Free-text
// summary on the non-reasoning model → append the reflective self-check to cut
// factual errors/omissions (arXiv:2507.02778).
func callYoutubeSummarizer(ctx context.Context, system, prompt string, maxTokens int) (string, error) {
	sctx, cancel := context.WithTimeout(ctx, youtubeSummaryTimeout)
	defer cancel()
	return pilot.CallLocalLLM(sctx, system+"\n"+pilot.ReflectionDirective, prompt, maxTokens)
}

// splitTranscriptChunks slices the transcript into summarizer-sized chunks,
// capped at youtubeSummaryMaxChunks. Returns the chunks and the rune count of
// any dropped tail (0 when the whole transcript is covered).
func splitTranscriptChunks(runes []rune) (chunks []string, droppedRunes int) {
	for start := 0; start < len(runes) && len(chunks) < youtubeSummaryMaxChunks; start += youtubeSummaryChunkChars {
		end := start + youtubeSummaryChunkChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	covered := youtubeSummaryMaxChunks * youtubeSummaryChunkChars
	if len(runes) > covered {
		droppedRunes = len(runes) - covered
	}
	return chunks, droppedRunes
}

// summarizeTranscriptChunked fans one call out per chunk (bounded ≤4,
// request-ctx-scoped, indexed writes — no shared state), then assembles the
// per-구간 sections and a best-effort cross-chunk conclusion. Partial results
// survive: a failed chunk becomes an explicit gap note, and only all-failed
// returns an error (→ fallback excerpt path).
func summarizeTranscriptChunked(ctx context.Context, r *media.YouTubeResult, runes []rune) (string, error) {
	chunks, dropped := splitTranscriptChunks(runes)
	summaries := make([]string, len(chunks))
	errs := make([]error, len(chunks))
	var wg sync.WaitGroup
	for i := range chunks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					errs[i] = fmt.Errorf("chunk %d summarizer panic: %v", i+1, rec)
				}
			}()
			prompt := fmt.Sprintf("제목: %s\n채널: %s\n(전체 %d구간 중 %d구간 자막)\n\n자막:\n%s",
				r.Title, r.Channel, len(chunks), i+1, chunks[i])
			summaries[i], errs[i] = callYoutubeSummarizer(ctx, youtubeChunkSystemPrompt, prompt, youtubeSummaryMaxTokens)
		}(i)
	}
	wg.Wait()

	var b strings.Builder
	ok := 0
	for i := range chunks {
		fmt.Fprintf(&b, "## 구간 %d/%d\n\n", i+1, len(chunks))
		if errs[i] != nil || strings.TrimSpace(summaries[i]) == "" {
			b.WriteString("_이 구간 요약은 생성하지 못했습니다 — 전체 자막은 spillover에 보존되어 있습니다._\n\n")
			continue
		}
		b.WriteString(strings.TrimSpace(summaries[i]))
		b.WriteString("\n\n")
		ok++
	}
	if ok == 0 {
		return "", fmt.Errorf("all %d transcript chunks failed to summarize", len(chunks))
	}
	if dropped > 0 {
		fmt.Fprintf(&b, "_[참고: 전사 뒤쪽 %d자는 구간 상한(%d구간)을 넘어 요약에서 제외 — 전체 자막은 spillover 참조.]_\n\n",
			dropped, youtubeSummaryMaxChunks)
	}
	// Best-effort conclusion over the assembled sections; skipped on error.
	if conclusion, err := callYoutubeSummarizer(ctx, youtubeConclusionSystemPrompt, b.String(), youtubeConclusionMaxTokens); err == nil && strings.TrimSpace(conclusion) != "" {
		b.WriteString("## 핵심 결론\n\n")
		b.WriteString(strings.TrimSpace(conclusion))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// storeYouTubeTranscript writes the full formatted result to spillover and
// returns its ID (empty when no store is wired or the write fails).
func storeYouTubeTranscript(ctx context.Context, spill tooldeps.SpilloverStore, r *media.YouTubeResult) string {
	if spill == nil {
		return ""
	}
	sessionKey := toolport.SessionKeyFromContext(ctx)
	spillID, err := spill.Store(sessionKey, "web", media.FormatYouTubeResult(r))
	if err != nil {
		return ""
	}
	return spillID
}

func formatYouTubeSummary(r *media.YouTubeResult, summary, spillID string) string {
	var b strings.Builder
	b.WriteString(media.FormatYouTubeMeta(r))
	b.WriteString("\n### 요약\n\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n")
	b.WriteString(spilloverNote(r, spillID))
	return b.String()
}

func formatYouTubeFallback(r *media.YouTubeResult, spillID string) string {
	excerpt := r.Transcript
	total := utf8.RuneCountInString(excerpt)
	truncated := false
	if total > youtubeFallbackExcerptChars {
		excerpt = string([]rune(excerpt)[:youtubeFallbackExcerptChars])
		truncated = true
	}

	var b strings.Builder
	b.WriteString(media.FormatYouTubeMeta(r))
	b.WriteString("\n### 자막 (일부 — 로컬 요약 모델 사용 불가)\n\n")
	b.WriteString(excerpt)
	b.WriteString("\n")
	if truncated {
		fmt.Fprintf(&b, "\n[자막이 %d자에서 잘렸습니다.]\n", youtubeFallbackExcerptChars)
	}
	b.WriteString(spilloverNote(r, spillID))
	return b.String()
}

func spilloverNote(r *media.YouTubeResult, spillID string) string {
	if spillID == "" {
		return ""
	}
	return fmt.Sprintf("\n_전체 자막(%d자)은 컨텍스트 절약을 위해 보관됨. 정확한 인용이 필요하면 read_spillover(spill_id=%q)로 조회하세요._\n",
		utf8.RuneCountInString(r.Transcript), spillID)
}
