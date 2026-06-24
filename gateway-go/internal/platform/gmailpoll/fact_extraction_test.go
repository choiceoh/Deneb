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

func TestStripWikiFactsBlock(t *testing.T) {
	block := renderFactsBlock([]WikiFactProposal{
		{Entity: "ABC상사", Type: "deal", Fact: "NDA 진행 70%"},
		{Entity: "박부장", Type: "person", Fact: "결정권자"},
	})
	if block == "" {
		t.Fatal("precondition: renderFactsBlock returned empty")
	}
	// Prose deliberately contains a blank line so the stripper can't just split
	// on the first "\n\n". The 📎 note mirrors the attachment-truncation line
	// synthesizeAnalysis appends after the (now-removed) facts block.
	prose := "**핵심**: 다음 주 화요일 회의.\n\n- 초안 공유 필요"
	note := "📎 분량이 커 일부만 반영된 첨부: 계약서.pdf"

	t.Run("block at tail is removed", func(t *testing.T) {
		if got := StripWikiFactsBlock(prose + "\n\n" + block); got != prose {
			t.Errorf("want prose only:\n%q\ngot:\n%q", prose, got)
		}
	})

	t.Run("block between prose and note keeps both", func(t *testing.T) {
		got := StripWikiFactsBlock(prose + "\n\n" + block + "\n\n" + note)
		want := prose + "\n\n" + note
		if got != want {
			t.Errorf("want prose+note:\n%q\ngot:\n%q", want, got)
		}
		if strings.Contains(got, wikiFactsBlockMarker) {
			t.Errorf("marker should be gone, got:\n%q", got)
		}
	})

	t.Run("text without block is unchanged", func(t *testing.T) {
		if got := StripWikiFactsBlock(prose); got != prose {
			t.Errorf("want unchanged:\n%q\ngot:\n%q", prose, got)
		}
	})

	t.Run("block-only input becomes empty", func(t *testing.T) {
		if got := StripWikiFactsBlock(block); got != "" {
			t.Errorf("want empty, got:\n%q", got)
		}
	})
}
