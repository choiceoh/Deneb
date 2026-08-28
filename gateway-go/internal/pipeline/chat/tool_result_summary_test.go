package chat

import (
	"fmt"
	"strings"
	"testing"
)

func TestSummarizeToolResultRendersOneChipLine(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		result string
		want   string
	}{
		"single line stands alone":      {result: "5개 파일", want: "5개 파일"},
		"multi line reports its size":   {result: "a.ts\nb.ts\nc.ts", want: "a.ts · 3줄"},
		"blank lines do not count":      {result: "\n\nonly\n\n", want: "only"},
		"leading blanks pick real head": {result: "\n  results:\nx\n", want: "results: · 2줄"},
		"inner whitespace collapses":    {result: "ok    done", want: "ok done"},
		"escape dropped, CR breaks":     {result: "clean\x1b[0m\rline", want: "clean[0m · 2줄"},
		"empty result yields nothing":   {result: "   \n\t\n", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := summarizeToolResult(tc.result); got != tc.want {
				t.Errorf("summarizeToolResult(%q) = %q, want %q", tc.result, got, tc.want)
			}
		})
	}
}

func TestSummarizeToolResultCapsLongHeadInRunes(t *testing.T) {
	t.Parallel()
	// Korean counts as runes, not bytes: a byte cap would cut this to a third
	// of the intended width.
	long := ""
	for range 80 {
		long += "가"
	}
	got := summarizeToolResult(long)
	runes := []rune(got)
	if len(runes) != toolSummaryMaxRunes+1 { // + the ellipsis
		t.Fatalf("summary length = %d runes, want %d", len(runes), toolSummaryMaxRunes+1)
	}
	if runes[len(runes)-1] != '…' {
		t.Errorf("summary %q does not end with an ellipsis", got)
	}
}

func TestToolResultPreviewKeepsReadableHeadWithinBounds(t *testing.T) {
	t.Parallel()
	t.Run("keeps line structure", func(t *testing.T) {
		t.Parallel()
		got := toolResultPreview("a.ts\nb.ts\nc.ts")
		if got != "a.ts\nb.ts\nc.ts" {
			t.Errorf("preview = %q", got)
		}
	})
	t.Run("caps the line count and marks the cut", func(t *testing.T) {
		t.Parallel()
		var b strings.Builder
		for i := range toolPreviewMaxLines + 10 {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		got := toolResultPreview(b.String())
		lines := strings.Split(got, "\n")
		if len(lines) != toolPreviewMaxLines+1 { // + the ellipsis line
			t.Fatalf("preview has %d lines, want %d", len(lines), toolPreviewMaxLines+1)
		}
		if lines[len(lines)-1] != "…" {
			t.Errorf("preview does not end with the cut marker: %q", lines[len(lines)-1])
		}
	})
	t.Run("caps runes for a single long line", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("가", toolPreviewMaxRunes+50)
		got := toolResultPreview(long)
		if runes := []rune(strings.TrimSuffix(got, "\n…")); len(runes) != toolPreviewMaxRunes {
			t.Errorf("preview body = %d runes, want %d", len(runes), toolPreviewMaxRunes)
		}
	})
	t.Run("drops control characters but keeps tabs", func(t *testing.T) {
		t.Parallel()
		if got := toolResultPreview("col\tval\x1b[0m"); got != "col\tval[0m" {
			t.Errorf("preview = %q", got)
		}
	})
	t.Run("blank result yields nothing", func(t *testing.T) {
		t.Parallel()
		if got := toolResultPreview("  \n\t\n"); got != "" {
			t.Errorf("preview = %q, want empty", got)
		}
	})
}
