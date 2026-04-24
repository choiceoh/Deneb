package insights

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMarkdownV2Basic(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	rep := &Report{
		Days:        7,
		Since:       now.Add(-7 * 24 * time.Hour),
		GeneratedAt: now,
		Overview: Overview{
			Sessions:     3,
			ActiveNow:    1,
			InputTokens:  12_345,
			OutputTokens: 678,
			TotalTokens:  13_023,
		},
		Models: []ModelStat{
			{Model: "gpt-4.1-turbo", Sessions: 2, TotalTokens: 10_000},
			{Model: "unknown", Sessions: 1, TotalTokens: 3_023},
		},
		TopSessions: []SessionStat{
			{Key: "tg:alice", Channel: "telegram", TotalTokens: 10_000},
		},
		Providers: []ProviderStat{
			{Provider: "openai", Calls: 42, Input: 12_000, Output: 3_000},
		},
	}

	md := RenderMarkdownV2(rep)

	if len(md) > 4096 {
		t.Fatalf("rendered output exceeds Telegram 4096-char limit: got %d", len(md))
	}
	// Korean labels present.
	for _, want := range []string{"사용 리포트", "요약", "상위 모델", "상위 세션", "프로바이더"} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q label:\n%s", want, md)
		}
	}
	// Code fences match up (even count).
	if n := strings.Count(md, "```"); n%2 != 0 {
		t.Errorf("unmatched code fences (%d) — Telegram will reject:\n%s", n, md)
	}
}

func TestRenderMarkdownV2EscapesSpecials(t *testing.T) {
	rep := &Report{
		Days:        1,
		GeneratedAt: time.Now(),
		Empty:       true,
		SchemaNotes: []string{"test. with! every* char- (yes)"},
	}
	out := RenderMarkdownV2(rep)
	// Every special in the note must be backslash-escaped.
	// Look for the escaped dot and bang.
	if !strings.Contains(out, `\.`) || !strings.Contains(out, `\!`) {
		t.Errorf("MarkdownV2 specials not escaped:\n%s", out)
	}
}

func TestRenderMarkdownV2Truncation(t *testing.T) {
	// Build a report with a ton of "top sessions" to force truncation.
	rep := &Report{
		Days:        30,
		GeneratedAt: time.Now(),
		Overview:    Overview{Sessions: 500, TotalTokens: 1},
	}
	for i := range 500 {
		rep.TopSessions = append(rep.TopSessions, SessionStat{
			Key:         strings.Repeat("x", 20),
			Channel:     "telegram",
			TotalTokens: int64(10_000 + i),
		})
	}
	md := RenderMarkdownV2(rep)
	if len(md) > 4096 {
		t.Fatalf("truncation failed: len=%d > 4096", len(md))
	}
	// Should contain the truncation marker.
	if !strings.Contains(md, "길이 제한") {
		start := len(md) - 200
		if start < 0 {
			start = 0
		}
		t.Errorf("expected truncation marker in output; got:\n%s", md[start:])
	}
	// Code fences must still be balanced.
	if n := strings.Count(md, "```"); n%2 != 0 {
		t.Errorf("unmatched code fences after truncation (%d)", n)
	}
}

func TestEscapeMDv2AllSpecials(t *testing.T) {
	in := `_*[]()~` + "`" + `>#+-=|{}.!\`
	out := escapeMDv2(in)
	// Every rune in the input must have a preceding backslash.
	if len(out) != len(in)*2 {
		t.Errorf("expected each char escaped; got %q -> %q (len %d vs %d)", in, out, len(in), len(out))
	}
}

func TestEscapeMDv2EmptyString(t *testing.T) {
	if got := escapeMDv2(""); got != "" {
		t.Errorf("empty input should yield empty output, got %q", got)
	}
}

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
	_ = RenderMarkdownV2(nil)
	_ = RenderPlain(nil)
}
