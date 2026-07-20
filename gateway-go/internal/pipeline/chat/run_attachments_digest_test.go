package chat

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSplitRunesProducesAlignedChunkRanges(t *testing.T) {
	rest := []rune(strings.Repeat("가", 45))
	chunks := splitRunes(rest, 100, 20, 30)
	if len(chunks) != 3 {
		t.Fatalf("len = %d, want 3", len(chunks))
	}
	// Offsets are relative to the original text (base 100), contiguous, and
	// the final chunk carries the remainder.
	wants := []struct{ start, end, textLen int }{
		{100, 120, 20}, {120, 140, 20}, {140, 145, 5},
	}
	for k, w := range wants {
		c := chunks[k]
		if c.start != w.start || c.end != w.end || len([]rune(c.text)) != w.textLen {
			t.Errorf("chunk %d = {start:%d end:%d len:%d}, want %+v", k, c.start, c.end, len([]rune(c.text)), w)
		}
	}

	// The chunk cap truncates coverage rather than growing unbounded.
	capped := splitRunes([]rune(strings.Repeat("나", 100)), 0, 10, 3)
	if len(capped) != 3 || capped[2].end != 30 {
		t.Errorf("cap violated: %+v", capped)
	}
}

func TestDigestOversizedDocumentKeepsSmallTextVerbatim(t *testing.T) {
	small := strings.Repeat("본문 ", 100)
	if got := digestOversizedDocument(context.Background(), "d", small, docInlineRuneCap, nil); got != small {
		t.Errorf("small document mutated")
	}
}

func TestDigestOversizedDocumentTruncatesVisiblyWithoutSummarizer(t *testing.T) {
	big := strings.Repeat("가", docInlineRuneCap+500)
	got := digestOversizedDocument(context.Background(), "계약서", big, docInlineRuneCap, nil)
	if r := []rune(got); len(r) > docInlineRuneCap+200 {
		t.Errorf("truncation did not shrink the document: %d runes", len(r))
	}
	if !strings.Contains(got, "[문서가 길어 앞") || !strings.Contains(got, "(경량 모델 미가용)]") {
		t.Errorf("truncation notice missing from tail: %q", got[len(got)-200:])
	}
}

func TestDigestOversizedDocumentAssemblesHeadAndChunkSummaries(t *testing.T) {
	// Head + 2 full chunks + remainder; the fake summarizer fails chunk 2 to
	// prove per-chunk failures stay visible instead of vanishing.
	big := strings.Repeat("가", digestHeadRunes+digestChunkRunes*2+300)
	var calls atomic.Int32
	fake := func(_ context.Context, system, user string, _ int) (string, error) {
		calls.Add(1)
		if system != digestSystemPrompt {
			t.Errorf("unexpected system prompt: %q", system)
		}
		if strings.Contains(user, fmt.Sprintf("%d–%d자", digestHeadRunes+1, digestHeadRunes+digestChunkRunes)) {
			return "· 첫 구간 요약", nil
		}
		return "", fmt.Errorf("simulated failure")
	}

	// inlineCap below the document size so the digest path actually fires.
	got := digestOversizedDocument(context.Background(), "IM", big, 30_000, fake)
	if calls.Load() != 3 {
		t.Errorf("summarizer calls = %d, want 3", calls.Load())
	}
	for _, want := range []string{
		"[대용량 문서",
		"━━ 서두 원문",
		"· 첫 구간 요약",
		"[이 구간은 요약에 실패해 내용이 비어 있음]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q", want)
		}
	}
	// The digest must be dramatically smaller than the original.
	if len([]rune(got)) >= len([]rune(big))/2 {
		t.Errorf("digest did not compress: %d runes", len([]rune(got)))
	}
}

func TestDigestOversizedDocumentFallsBackWhenAllChunksFail(t *testing.T) {
	big := strings.Repeat("가", docInlineRuneCap+500)
	fake := func(context.Context, string, string, int) (string, error) {
		return "", fmt.Errorf("local AI down")
	}
	got := digestOversizedDocument(context.Background(), "d", big, docInlineRuneCap, fake)
	if !strings.Contains(got, "(구간 요약 실패)]") {
		t.Errorf("all-fail digest must degrade to visible truncation, got tail %q", got[len(got)-200:])
	}
}
