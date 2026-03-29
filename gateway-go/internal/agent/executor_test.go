package agent

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestExtractThinkingSummary_Basic(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."},
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingSummary(blocks)
	if got != "먼저 파일을 읽어봐야겠습니다." {
		t.Errorf("unexpected summary: %q", got)
	}
}

func TestExtractThinkingSummary_Empty(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingSummary(blocks)
	if got != "" {
		t.Errorf("expected empty summary, got: %q", got)
	}
}

func TestExtractThinkingSummary_Truncation(t *testing.T) {
	// Build a very long line (>60 runes).
	longLine := "이것은 매우 긴 추론 텍스트입니다. 이 텍스트는 60자를 초과하여 잘려야 합니다. 추가 텍스트가 더 있습니다."
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: longLine},
	}

	got := extractThinkingSummary(blocks)
	runes := []rune(got)
	// 60 runes + "…" = 61 runes
	if len(runes) > 61 {
		t.Errorf("expected truncated summary (max 61 runes), got %d runes: %q", len(runes), got)
	}
	if got[len(got)-3:] != "…" {
		t.Errorf("expected trailing ellipsis, got: %q", got)
	}
}

func TestExtractThinkingSummary_SkipsCodeFences(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "파일을 수정해야 합니다\n```go\nfunc main() {}\n```"},
	}

	got := extractThinkingSummary(blocks)
	if got == "```" || got == "```go" {
		t.Errorf("should skip code fences, got: %q", got)
	}
}

func TestExtractThinkingSummary_StripsMarkers(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "분석 중\n- 이 파일을 읽어야 합니다"},
	}

	got := extractThinkingSummary(blocks)
	if got != "이 파일을 읽어야 합니다" {
		t.Errorf("expected markers stripped, got: %q", got)
	}
}
