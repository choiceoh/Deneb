package agent

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestExtractThinkingText_Basic(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."},
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingText(blocks)
	want := "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."
	if got != want {
		t.Errorf("unexpected text: %q", got)
	}
}

func TestExtractThinkingText_Empty(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingText(blocks)
	if got != "" {
		t.Errorf("expected empty, got: %q", got)
	}
}

func TestExtractThinkingText_MultipleBlocks(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "first thinking block"},
		{Type: "text", Text: "some text"},
		{Type: "thinking", Thinking: "second thinking block — closer to tools"},
		{Type: "tool_use", Name: "exec"},
	}

	got := extractThinkingText(blocks)
	if got != "second thinking block — closer to tools" {
		t.Errorf("expected last thinking block, got: %q", got)
	}
}
