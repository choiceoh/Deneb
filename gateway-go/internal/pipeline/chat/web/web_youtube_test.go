package web

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

func sampleResult(transcript string) *media.YouTubeResult {
	return &media.YouTubeResult{
		Title:      "테스트 영상",
		Channel:    "테스트 채널",
		Duration:   "10:00",
		URL:        "https://youtu.be/abcdefghijk",
		Language:   "ko",
		Transcript: transcript,
	}
}

// Short transcripts pass through unchanged — no summarization, no spillover note.
func TestSummarizeYouTubeResult_ShortTranscriptPassthrough(t *testing.T) {
	r := sampleResult("짧은 자막입니다.")
	out := summarizeYouTubeResult(context.Background(), nil, r)

	if !strings.Contains(out, "### 자막") {
		t.Fatalf("expected raw transcript section, got:\n%s", out)
	}
	if strings.Contains(out, "### 요약") {
		t.Fatalf("short transcript should not be summarized, got:\n%s", out)
	}
}

// A result without a usable transcript passes through unchanged.
func TestSummarizeYouTubeResult_NoTranscript(t *testing.T) {
	r := sampleResult("")
	out := summarizeYouTubeResult(context.Background(), nil, r)

	if !strings.Contains(out, "(자막 없음)") {
		t.Fatalf("expected no-transcript marker, got:\n%s", out)
	}
}

func TestFormatYouTubeSummary(t *testing.T) {
	r := sampleResult(strings.Repeat("가", 5000))
	out := formatYouTubeSummary(r, "  핵심 요약 내용  ", "sp_123")

	if !strings.Contains(out, "### 요약") {
		t.Errorf("missing summary header:\n%s", out)
	}
	if !strings.Contains(out, "핵심 요약 내용") {
		t.Errorf("missing summary body:\n%s", out)
	}
	if strings.Contains(out, "가가가") {
		t.Errorf("full transcript must not appear inline:\n%s", out)
	}
	if !strings.Contains(out, "read_spillover(spill_id=\"sp_123\")") {
		t.Errorf("missing spillover reference:\n%s", out)
	}
}

func TestFormatYouTubeFallback_TruncatesAndReferencesSpillover(t *testing.T) {
	r := sampleResult(strings.Repeat("나", youtubeFallbackExcerptChars+500))
	out := formatYouTubeFallback(r, "sp_xyz")

	if !strings.Contains(out, "로컬 요약 모델 사용 불가") {
		t.Errorf("missing fallback marker:\n%s", out)
	}
	if !strings.Contains(out, "잘렸습니다") {
		t.Errorf("expected truncation note:\n%s", out)
	}
	if !strings.Contains(out, "read_spillover(spill_id=\"sp_xyz\")") {
		t.Errorf("missing spillover reference:\n%s", out)
	}
	// The inline excerpt must be bounded, not the full transcript.
	if strings.Count(out, "나") > youtubeFallbackExcerptChars {
		t.Errorf("inline excerpt exceeds bound: %d runes", strings.Count(out, "나"))
	}
}

// The full transcript is stored to spillover and retrievable by ID.
func TestStoreYouTubeTranscript_RoundTrip(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	ctx := toolctx.WithSessionKey(context.Background(), "sess-1")
	r := sampleResult(strings.Repeat("다", 4000))

	spillID := storeYouTubeTranscript(ctx, store, r)
	if spillID == "" {
		t.Fatal("expected non-empty spill ID")
	}

	full, err := store.Load(spillID, "sess-1")
	if err != nil {
		t.Fatalf("load spilled transcript: %v", err)
	}
	if !strings.Contains(full, r.Transcript) {
		t.Error("spilled content missing full transcript")
	}
}

// A nil store degrades gracefully (no spill ID, no panic).
func TestStoreYouTubeTranscript_NilStore(t *testing.T) {
	if id := storeYouTubeTranscript(context.Background(), nil, sampleResult("x")); id != "" {
		t.Fatalf("expected empty ID with nil store, got %q", id)
	}
}
