// web_youtube.go — YouTube transcript summarization.
//
// YouTube transcripts run to tens of thousands of characters. Dropping the full
// transcript into the conversation transcript is exactly what makes "summarize a
// few YouTube links" overflow the protected context window: each result lands in
// the recent (non-compactable) turns, and a handful of them exceed the budget
// before compaction can touch anything (see .claude/rules/prompt-cache.md §5 +
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
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

const (
	// youtubeSummarizeMinChars — transcripts shorter than this stay inline;
	// summarizing tiny captions adds latency without meaningfully saving context.
	youtubeSummarizeMinChars = 2000
	// youtubeSummaryInputMaxChars caps the transcript fed to the summarizer
	// prompt to fit the lightweight local model. The full text is always
	// preserved in spillover, so this only bounds the summarization input.
	youtubeSummaryInputMaxChars = 32000
	// youtubeSummaryMaxTokens is the token budget for the generated summary.
	youtubeSummaryMaxTokens = 1200
	// youtubeFallbackExcerptChars bounds the inline excerpt kept when the local
	// summarizer is unavailable.
	youtubeFallbackExcerptChars = 6000
	// youtubeSummaryTimeout is the per-link summarization deadline.
	youtubeSummaryTimeout = 60 * time.Second
)

const youtubeSummarySystemPrompt = "당신은 유튜브 영상 자막을 한국어로 요약하는 전문가입니다. " +
	"핵심 주제, 주요 논점, 결론을 구조적으로 정리하세요. " +
	"불필요한 서두 없이 요약 내용만 바로 출력하세요. " +
	"중요한 수치·이름·인용은 보존하세요."

// summarizeYouTubeResult turns a raw YouTube extraction into a compact result
// for the conversation transcript: metadata + a generated summary, with the
// full transcript offloaded to spillover. Short or transcript-less results pass
// through unchanged.
func summarizeYouTubeResult(ctx context.Context, spill *agent.SpilloverStore, r *media.YouTubeResult) string {
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

// summarizeTranscript runs the isolated local-LLM summarization call.
func summarizeTranscript(ctx context.Context, r *media.YouTubeResult) (string, error) {
	// Skip the call when the local AI was recently confirmed down — avoids N
	// sequential timeouts for a batch of links.
	if pilot.LocalAIRecentlyDown() {
		return "", fmt.Errorf("local summarizer unavailable")
	}
	sctx, cancel := context.WithTimeout(ctx, youtubeSummaryTimeout)
	defer cancel()

	transcript := r.Transcript
	if utf8.RuneCountInString(transcript) > youtubeSummaryInputMaxChars {
		transcript = string([]rune(transcript)[:youtubeSummaryInputMaxChars])
	}
	prompt := fmt.Sprintf("제목: %s\n채널: %s\n\n자막:\n%s", r.Title, r.Channel, transcript)
	return pilot.CallLocalLLM(sctx, youtubeSummarySystemPrompt, prompt, youtubeSummaryMaxTokens)
}

// storeYouTubeTranscript writes the full formatted result to spillover and
// returns its ID (empty when no store is wired or the write fails).
func storeYouTubeTranscript(ctx context.Context, spill *agent.SpilloverStore, r *media.YouTubeResult) string {
	if spill == nil {
		return ""
	}
	sessionKey := toolctx.SessionKeyFromContext(ctx)
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
