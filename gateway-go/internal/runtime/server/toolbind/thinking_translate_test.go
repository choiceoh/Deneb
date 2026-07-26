package toolbind

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// swapTranslator replaces the DeepL binding for one test and restores it after.
func swapTranslator(t *testing.T, fn func(context.Context, []string, string) ([]string, error)) {
	t.Helper()
	prev := TranslateSegments
	TranslateSegments = fn
	t.Cleanup(func() { TranslateSegments = prev })
}

func TestTranslateThinkingPreservesLineStructure(t *testing.T) {
	// The blockquote renderer turns every newline into a quote marker, so a
	// translator that drops or adds lines collapses the rendered block.
	const in = "first line\n\nsecond line\n\n\nthird line"
	var got []string
	swapTranslator(t, func(_ context.Context, segments []string, lang string) ([]string, error) {
		if lang != "Korean" {
			t.Fatalf("target language = %q, want Korean", lang)
		}
		got = segments
		out := make([]string, len(segments))
		for i, s := range segments {
			out[i] = "번역:" + s
		}
		return out, nil
	})

	out, ok := TranslateThinking(context.Background(), in)
	if !ok {
		t.Fatal("translation reported failure")
	}
	if len(got) != 3 {
		t.Fatalf("sent %d segments, want 3 — blank lines carry no text and only spend quota: %q", len(got), got)
	}
	if strings.Count(out, "\n") != strings.Count(in, "\n") {
		t.Fatalf("line count changed:\n in: %q\nout: %q", in, out)
	}
	for _, want := range []string{"번역:first line", "번역:second line", "번역:third line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestTranslateThinkingSurvivesPartialFailure(t *testing.T) {
	// TranslateSegments defaults each segment to its own original text and
	// overwrites only on a clean batch, so one bad batch must degrade to "that
	// line stays English" rather than losing the block.
	swapTranslator(t, func(_ context.Context, segments []string, _ string) ([]string, error) {
		out := make([]string, len(segments))
		copy(out, segments)
		out[0] = "번역됨"
		return out, nil
	})
	out, ok := TranslateThinking(context.Background(), "alpha\nbeta")
	if !ok {
		t.Fatal("a partially translated block must still be delivered")
	}
	if out != "번역됨\nbeta" {
		t.Fatalf("got %q, want the translated first line and the original second", out)
	}
}

func TestTranslateThinkingReportsFailure(t *testing.T) {
	cases := map[string]func(context.Context, []string, string) ([]string, error){
		"error": func(context.Context, []string, string) ([]string, error) {
			return nil, errors.New("deepl down")
		},
		"length mismatch": func(context.Context, []string, string) ([]string, error) {
			return []string{"하나"}, nil
		},
		"nothing changed": func(_ context.Context, segments []string, _ string) ([]string, error) {
			// Every segment came back identical — claiming a translation that
			// did not happen would hide a dead DeepL binding behind English text
			// that looks intentional.
			out := make([]string, len(segments))
			copy(out, segments)
			return out, nil
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			swapTranslator(t, fn)
			if _, ok := TranslateThinking(context.Background(), "alpha\nbeta"); ok {
				t.Fatal("expected failure to be reported so the caller keeps the original")
			}
		})
	}
}

func TestTranslateThinkingIgnoresBlankInput(t *testing.T) {
	swapTranslator(t, func(context.Context, []string, string) ([]string, error) {
		t.Fatal("translator must not be called for text with no content")
		return nil, nil
	})
	if _, ok := TranslateThinking(context.Background(), "\n  \n\n"); ok {
		t.Fatal("blank thinking must report failure, not an empty translation")
	}
}

func TestTranslateThinkingSkipsBlocksItShouldNotTouch(t *testing.T) {
	// This layer owns the policy for BOTH surfaces (the SSE done frame and the
	// 🧠 blockquote), so the skip rules are asserted here and nowhere else.
	swapTranslator(t, func(context.Context, []string, string) ([]string, error) {
		t.Fatal("translator must not be called for a block the policy rejects")
		return nil, nil
	})
	cases := map[string]string{
		"already korean": "먼저 첨부 파일 경로를 확인한다. 빈 ID로 불리면 실패한다.",
		"blank":          "\n   \n",
		// A pathological run must not turn into an unbounded translation bill.
		"over the byte cap": strings.Repeat("the code sets AttachmentID to empty. ", 1000),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := TranslateThinking(context.Background(), in); ok {
				t.Fatal("expected the policy to decline")
			}
		})
	}
}

func TestHangulRatioIgnoresNonLetters(t *testing.T) {
	// Code, punctuation, and digits must not count. One 13-letter identifier
	// drags a 9-letter Korean sentence down to ~0.41, which is precisely why the
	// skip bar is 0.30 and not 0.50: set it higher and ordinary Korean reasoning
	// about code gets re-translated.
	cases := []struct {
		in               string
		wantMin, wantMax float64
	}{
		{"이 함수는 `GetAttachment` 를 호출한다.", 0.35, 1.0},
		{"the current code sets AttachmentID to empty", 0, 0},
		{"1234 :: {} --- <>", 0, 0},
		{"", 0, 0},
	}
	for _, tc := range cases {
		got := hangulRatio(tc.in)
		if got < tc.wantMin || got > tc.wantMax {
			t.Fatalf("hangulRatio(%q) = %.2f, want within [%.2f, %.2f]", tc.in, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestTranslateThinkingBoundsItsOwnDeadline(t *testing.T) {
	// No caller can forget it: a hung translator must never hold a turn open
	// for a display nicety.
	swapTranslator(t, func(ctx context.Context, segments []string, _ string) ([]string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("TranslateSegments was called without a deadline")
		}
		out := make([]string, len(segments))
		for i := range segments {
			out[i] = "번역됨"
		}
		return out, nil
	})
	if _, ok := TranslateThinking(context.Background(), "alpha"); !ok {
		t.Fatal("expected success")
	}
}
