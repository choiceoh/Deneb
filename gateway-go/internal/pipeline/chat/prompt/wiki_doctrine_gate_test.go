package prompt

import (
	"strings"
	"testing"
)

// 사용자 모델 갱신 / 사내 고유 지식 / 작업 기억 all instruct wiki(write|search)
// and knowledge. The coding and verifier presets carry no wiki at all — the
// coding profile deliberately withholds the 업무 surface — so shipping them is
// ~1.4K of instruction the run cannot follow, which is what the polaris block
// in the same file warns produces failed tool-call loops.
func TestWikiDoctrineBlocksGateOnTheWikiTool(t *testing.T) {
	headings := []string{"## 분석 → 위키 갱신", "## 사용자 모델 갱신", "## 사내 고유 지식", "## 작업 기억"}

	withWiki := BuildSystemPrompt(SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "wiki"}, {Name: "read"}},
	})
	for _, h := range headings {
		if !strings.Contains(withWiki, h) {
			t.Errorf("a run WITH wiki lost %q", h)
		}
	}

	// The coding surface: file/shell tools, no wiki.
	withoutWiki := BuildSystemPrompt(SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}, {Name: "grep"}, {Name: "exec"}, {Name: "write"}},
	})
	for _, h := range headings {
		if strings.Contains(withoutWiki, h) {
			t.Errorf("a run WITHOUT wiki still got %q", h)
		}
	}
	// It must not mention the tool at all in those doctrines.
	if strings.Contains(withoutWiki, `wiki(action="write"`) {
		t.Errorf("a wiki-less run was told to call wiki(action=\"write\")")
	}
	// The rest of the prompt survives — this gates three blocks, not the file.
	for _, keep := range []string{"## Role", "## Communication", "## Tooling"} {
		if !strings.Contains(withoutWiki, keep) {
			t.Errorf("gating dropped %q", keep)
		}
	}
}
