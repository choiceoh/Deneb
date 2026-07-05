package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseTranslations(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
		ok   bool
		out  []string
	}{
		{"clean array", `["안녕","세계"]`, 2, true, []string{"안녕", "세계"}},
		{"code fenced", "```json\n[\"하나\",\"둘\"]\n```", 2, true, []string{"하나", "둘"}},
		{"envelope", `{"translations":["가","나","다"]}`, 3, true, []string{"가", "나", "다"}},
		{"count short", `["하나"]`, 2, false, nil},
		{"count long", `["하나","둘","셋"]`, 2, false, nil},
		{"garbage", `not json at all`, 1, false, nil},
		{"empty array vs want", `[]`, 1, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTranslations(tc.raw, tc.want)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v (raw=%q)", ok, tc.ok, tc.raw)
			}
			if ok {
				if len(got) != len(tc.out) {
					t.Fatalf("len=%d want %d", len(got), len(tc.out))
				}
				for i := range got {
					if got[i] != tc.out[i] {
						t.Fatalf("got[%d]=%q want %q", i, got[i], tc.out[i])
					}
				}
			}
		})
	}
}

// TestTranslateSegments_EmptyAndCountInvariant checks the structural guarantees
// that don't need a live model: empty input is a no-op, and the function always
// returns a slice the SAME length as its input (the index-replacement contract).
func TestTranslateSegments_Empty(t *testing.T) {
	out, err := TranslateSegments(context.Background(), nil, "Korean")
	if err != nil || out != nil {
		t.Fatalf("empty input: got out=%v err=%v, want nil,nil", out, err)
	}
}

func TestBuildTranslatePrompt_AnchorsCountAndSegments(t *testing.T) {
	system, user := buildTranslatePrompt([]translateInput{
		{Text: "Hello"},
		{Text: "Привет", Context: "Привет мир", Role: "body"},
	}, "Korean")
	if system == "" || user == "" {
		t.Fatal("expected non-empty prompt")
	}
	// The user message must carry the exact count and the raw segments so the
	// model is anchored to return the same number of items.
	for _, want := range []string{"exactly 2", "Hello", "Привет", "Привет мир"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, user)
		}
	}
}

func TestTranslateSegments_EnvelopeFallbackKeepsOriginalText(t *testing.T) {
	old := translateBatchFn
	translateBatchFn = func(context.Context, []translateInput, string) ([]string, bool) {
		return nil, false
	}
	defer func() { translateBatchFn = old }()

	raw := translateSegmentEnvelopePrefix + `{"text":"Hello","context":"Hello world","role":"body"}`
	out, err := TranslateSegments(context.Background(), []string{raw}, "Korean")
	if err != nil {
		t.Fatalf("TranslateSegments error: %v", err)
	}
	if len(out) != 1 || out[0] != "Hello" {
		t.Fatalf("fallback out=%q, want original text only", out)
	}
}

func TestParseTranslationItems_GroupedParts(t *testing.T) {
	inputs := []translateInput{
		{Text: "Hello world", Parts: []string{"Hello ", "world"}},
		{Text: "Read more"},
	}
	out, ok := parseTranslationItems(`[["안녕 ","세계"],"더 보기"]`, inputs)
	if !ok {
		t.Fatal("parseTranslationItems returned !ok")
	}
	wantParts := translatePartsEnvelopePrefix + `["안녕 ","세계"]`
	if len(out) != 2 || out[0] != wantParts || out[1] != "더 보기" {
		t.Fatalf("out=%q want [%q %q]", out, wantParts, "더 보기")
	}
}

func TestTranslateSegments_TranslatesBatchesConcurrently(t *testing.T) {
	old := translateBatchFn
	defer func() { translateBatchFn = old }()

	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	calls := 0
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	translateBatchFn = func(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
		mu.Lock()
		calls++
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, false
		}
		out := make([]string, len(batch))
		for i, in := range batch {
			out[i] = "ko:" + in.Text
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		return out, true
	}

	segments := make([]string, 5)
	for i := range segments {
		segments[i] = strings.Repeat(fmt.Sprintf("%d", i), 900)
	}

	done := make(chan struct{})
	var out []string
	var err error
	go func() {
		out, err = TranslateSegments(context.Background(), segments, "Korean")
		close(done)
	}()

	for i := 0; i < translateMaxConcurrentBatches; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for concurrent batch %d", i+1)
		}
	}

	mu.Lock()
	gotMax := maxInFlight
	mu.Unlock()
	if gotMax != translateMaxConcurrentBatches {
		close(release)
		t.Fatalf("max concurrency=%d want %d", gotMax, translateMaxConcurrentBatches)
	}
	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TranslateSegments did not finish")
	}
	if err != nil {
		t.Fatalf("TranslateSegments error: %v", err)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != len(segments) {
		t.Fatalf("translate calls=%d want %d", gotCalls, len(segments))
	}
	for i := range segments {
		if want := "ko:" + segments[i]; out[i] != want {
			t.Fatalf("out[%d]=%q want %q", i, out[i], want)
		}
	}
}
