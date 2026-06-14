package insights

import (
	"strings"
	"testing"
	"time"
)

func TestFormatCount(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		999:       "999",
		1_000:     "1.0K",
		12_345:    "12.3K",
		1_000_000: "1.0M",
		-2_500:    "-2.5K",
	}
	for in, want := range cases {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short unchanged, got %q", got)
	}
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("truncate(hello world, 5) = %q; want hell…", got)
	}
	if got := truncate("안녕하세요", 3); got != "안녕…" {
		t.Errorf("korean truncate = %q; want 안녕…", got)
	}
}

func TestRenderPlainNonEmpty(t *testing.T) {
	rep := &Report{
		Days:        7,
		GeneratedAt: time.Now(),
		Overview:    Overview{Sessions: 1, InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Models:      []ModelStat{{Model: "gpt-4", Sessions: 1, TotalTokens: 150}},
	}
	out := RenderPlain(rep)
	for _, want := range []string{"요약", "상위 모델", "gpt-4"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderNilReport(t *testing.T) {
	// Must not panic.
	_ = RenderPlain(nil)
}
