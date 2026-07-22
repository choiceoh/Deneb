package chat

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSplitLineChunksProducesAlignedLineRanges(t *testing.T) {
	lines := make([]string, 9)
	for i := range lines {
		lines[i] = strings.Repeat("가", 10)
	}
	chunks := splitLineChunks(lines, 3, 25, 10)
	if len(chunks) != 3 {
		t.Fatalf("len = %d, want 3", len(chunks))
	}
	wantRanges := [][2]int{{4, 5}, {6, 7}, {8, 9}}
	for k, c := range chunks {
		if c.startLine != wantRanges[k][0] || c.endLine != wantRanges[k][1] {
			t.Errorf("chunk %d range = %d–%d, want %v", k, c.startLine, c.endLine, wantRanges[k])
		}
		// The alignment contract: chunk text is exactly those lines.
		if want := strings.Join(lines[c.startLine-1:c.endLine], "\n"); c.text != want {
			t.Errorf("chunk %d text misaligned with its line range", k)
		}
	}

	// A line larger than the chunk budget still lands in its own chunk, and
	// the chunk cap truncates coverage instead of growing unbounded.
	capped := splitLineChunks(lines, 0, 5, 2)
	if len(capped) != 2 || capped[0].startLine != 1 || capped[0].endLine != 1 {
		t.Errorf("cap/oversized-line handling: %+v", capped)
	}
}

func TestChunkTopicParsesTopicLine(t *testing.T) {
	if got := chunkTopic("주제: 재무 현황 요약\n· 매출 12억"); got != "재무 현황 요약" {
		t.Errorf("topic = %q", got)
	}
	if got := chunkTopic("그냥 첫 줄\n다음 줄"); got != "그냥 첫 줄" {
		t.Errorf("fallback topic = %q", got)
	}
}

func TestDigestOversizedDocumentKeepsSmallTextVerbatim(t *testing.T) {
	small := strings.TrimSpace(strings.Repeat("본문 ", 100))
	if got := digestOversizedDocument(context.Background(), "d", small, docInlineRuneCap, nil, digestSource{}); got != small {
		t.Errorf("small document mutated")
	}
}

func TestDigestOversizedDocumentTruncatesVisiblyWithoutSummarizer(t *testing.T) {
	big := strings.Repeat("가", docInlineRuneCap+500)
	got := digestOversizedDocument(context.Background(), "계약서", big, docInlineRuneCap, nil, digestSource{})
	if r := []rune(got); len(r) > docInlineRuneCap+200 {
		t.Errorf("truncation did not shrink the document: %d runes", len(r))
	}
	if !strings.Contains(got, "[문서가 길어 앞") || !strings.Contains(got, "(경량 모델 미가용)]") {
		t.Errorf("truncation notice missing from tail: %q", got[len(got)-200:])
	}

	// With an archived source, the notice points at it so the tail stays
	// reachable.
	got = digestOversizedDocument(context.Background(), "계약서", big, docInlineRuneCap, nil,
		digestSource{path: "/mem/captures/a.md", bodyLine: 8})
	if !strings.Contains(got, "원문 전체 보관: /mem/captures/a.md") {
		t.Errorf("truncation notice missing archive path: %q", got[len(got)-300:])
	}
}

func TestDigestOversizedDocumentBuildsLineMappedMap(t *testing.T) {
	// 500 lines × 100 runes ≈ 50K runes; inlineCap 20K forces the digest.
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("%03d ", i) + strings.Repeat("가", 96)
	}
	doc := strings.Join(lines, "\n")
	headN := headLineCount(strings.Split(doc, "\n"))
	src := digestSource{path: "/mem/captures/im.md", bodyLine: 8}

	var calls atomic.Int32
	fake := func(_ context.Context, system, user string, _ int) (string, error) {
		calls.Add(1)
		if system != digestSystemPrompt {
			t.Errorf("unexpected system prompt: %q", system)
		}
		// Fail every chunk but the first to prove failures stay visible.
		if strings.Contains(user, fmt.Sprintf("의 %d–", headN+1)) {
			return "주제: 첫 구간 지도\n· 사업비 1,240억원", nil
		}
		return "", fmt.Errorf("simulated failure")
	}

	got := digestOversizedDocument(context.Background(), "IM", doc, 20_000, fake, src)
	if calls.Load() < 2 {
		t.Fatalf("summarizer calls = %d, want ≥ 2", calls.Load())
	}
	for _, want := range []string{
		"[대용량 문서 지도 — 「IM」",
		"원문 전체 보관: /mem/captures/im.md",
		"━━ 목차 ━━",
		fmt.Sprintf("서두 원문: %d–%d줄", src.fileLine(1), src.fileLine(headN)),
		"— 첫 구간 지도",
		"(요약 실패)",
		"· 사업비 1,240억원",
		"[이 구간은 요약에 실패해 내용이 비어 있음",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("digest map missing %q", want)
		}
	}
	// TOC line numbers are FILE lines: chunk 1 starts right after the head,
	// shifted by the archive header (bodyLine 8 → +7).
	if want := fmt.Sprintf("구간 1: %d–", headN+1+7); !strings.Contains(got, want) {
		t.Errorf("chunk 1 TOC not mapped to file lines, want %q in:\n%.600s", want, got)
	}
	if len([]rune(got)) >= len([]rune(doc))/2 {
		t.Errorf("digest did not compress: %d runes", len([]rune(got)))
	}
}

func TestDigestOversizedDocumentFallsBackWhenAllChunksFail(t *testing.T) {
	big := strings.Repeat("가", docInlineRuneCap+500)
	fake := func(context.Context, string, string, int) (string, error) {
		return "", fmt.Errorf("local AI down")
	}
	got := digestOversizedDocument(context.Background(), "d", big, docInlineRuneCap, fake, digestSource{})
	if !strings.Contains(got, "(구간 요약 실패)]") {
		t.Errorf("all-fail digest must degrade to visible truncation, got tail %q", got[len(got)-200:])
	}
}

func TestSummarizeAttachmentPreviewShortTextIsVerbatim(t *testing.T) {
	short := "짧은 문서 본문. 요약할 필요 없음."
	var calls atomic.Int32
	fake := func(context.Context, string, string, int) (string, error) {
		calls.Add(1)
		return "요약", nil
	}
	// Text within the preview budget is returned whole — no model call.
	if got := summarizeAttachmentPreview(context.Background(), "d.txt", short, fake); got != short {
		t.Errorf("short text should pass through verbatim, got %q", got)
	}
	if calls.Load() != 0 {
		t.Errorf("summarizer should not be called for short text, calls=%d", calls.Load())
	}
}

func TestSummarizeAttachmentPreviewSummarizesLongText(t *testing.T) {
	big := strings.Repeat("가", previewSummaryRuneCap+500)
	var gotTokens int
	fake := func(_ context.Context, system, user string, maxTokens int) (string, error) {
		gotTokens = maxTokens
		if system != previewSummaryPrompt {
			t.Errorf("unexpected system prompt: %q", system)
		}
		if !strings.Contains(user, "「보고서.pdf」") {
			t.Errorf("user message should name the file: %q", user)
		}
		return "주제: 보고서 요약\n- 매출 12억", nil
	}
	got := summarizeAttachmentPreview(context.Background(), "보고서.pdf", big, fake)
	if got != "주제: 보고서 요약\n- 매출 12억" {
		t.Errorf("preview = %q", got)
	}
	if gotTokens != previewSummaryTokens {
		t.Errorf("maxTokens = %d, want %d", gotTokens, previewSummaryTokens)
	}
}

func TestSummarizeAttachmentPreviewBoundsHugeInput(t *testing.T) {
	huge := strings.Repeat("나", previewSummaryInputRunes+5_000)
	var gotInputRunes int
	fake := func(_ context.Context, _, user string, _ int) (string, error) {
		// user = header + "\n\n" + input; the input body is bounded to the head cap.
		if i := strings.Index(user, "\n\n"); i >= 0 {
			gotInputRunes = len([]rune(user[i+2:]))
		}
		if !strings.Contains(user, "앞부분") {
			t.Errorf("bounded input should be flagged as a head excerpt: %q", user[:80])
		}
		return "주제: 요약", nil
	}
	summarizeAttachmentPreview(context.Background(), "big.txt", huge, fake)
	if gotInputRunes != previewSummaryInputRunes {
		t.Errorf("model input runes = %d, want %d (bounded head)", gotInputRunes, previewSummaryInputRunes)
	}
}

func TestSummarizeAttachmentPreviewCapsOversizedSummary(t *testing.T) {
	big := strings.Repeat("가", previewSummaryRuneCap+500)
	fake := func(context.Context, string, string, int) (string, error) {
		return strings.Repeat("다", previewSummaryRuneCap+300), nil // model overshoots
	}
	got := summarizeAttachmentPreview(context.Background(), "d", big, fake)
	if r := []rune(got); len(r) != previewSummaryRuneCap+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("oversized summary should be rune-capped with an ellipsis, got %d runes", len([]rune(got)))
	}
}

func TestSummarizeAttachmentPreviewFallsBackOnFailure(t *testing.T) {
	big := strings.Repeat("가", previewSummaryRuneCap+500)
	cases := map[string]chunkSummarizer{
		"nil summarizer": nil,
		"error":          func(context.Context, string, string, int) (string, error) { return "", fmt.Errorf("down") },
		"empty":          func(context.Context, string, string, int) (string, error) { return "  ", nil },
		"placeholder": func(context.Context, string, string, int) (string, error) {
			return "(no response from local model)", nil
		},
	}
	for name, fake := range cases {
		if got := summarizeAttachmentPreview(context.Background(), "d", big, fake); got != "" {
			t.Errorf("%s: want empty (caller front-cuts), got %q", name, got)
		}
	}
}
