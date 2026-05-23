package gmailpoll

import (
	"context"
	"strings"
	"testing"
)

func TestExtractFactsForWiki_NoLocalClient(t *testing.T) {
	// No LocalClient → extractor must short-circuit with empty result, never panic.
	got := extractFactsForWiki(context.Background(), PipelineDeps{}, "임의의 분석 결과")
	if got != "" {
		t.Errorf("expected empty when LocalClient is nil, got %q", got)
	}
}

func TestExtractFactsForWiki_EmptyAnalysis(t *testing.T) {
	got := extractFactsForWiki(context.Background(), PipelineDeps{LocalModel: "x"}, "")
	if got != "" {
		t.Errorf("expected empty when analysis is empty, got %q", got)
	}
}

func TestExtractFactsForWiki_RenderingFormat(t *testing.T) {
	// Render path is reachable via a dummy bundle — we can't exercise the LLM
	// from a unit test, but the formatter is mechanical. Validate it by
	// constructing the proposal slice and running it through the same render
	// logic.
	facts := []WikiFactProposal{
		{Entity: "ABC상사", Type: "deal", Fact: "NDA 진행 70%"},
		{Entity: "박부장", Type: "person", Fact: "결정권자"},
		{Entity: "", Type: "deal", Fact: "skipped (no entity)"},
		{Entity: "X프로젝트", Type: "", Fact: "타입 없음 케이스"},
	}
	rendered := renderFactsBlock(facts)
	for _, want := range []string{
		"📝 위키 갱신 제안",
		"**ABC상사** (deal): NDA 진행 70%",
		"**박부장** (person): 결정권자",
		"**X프로젝트**: 타입 없음 케이스",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in rendered block:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "skipped") {
		t.Errorf("rendering should skip entries with empty Entity, got:\n%s", rendered)
	}
}
