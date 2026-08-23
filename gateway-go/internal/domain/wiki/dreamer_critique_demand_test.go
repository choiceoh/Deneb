package wiki

import (
	"strings"
	"testing"
)

func TestBuildCritiquePrompt_InjectsDemandOnlyWhenPresent(t *testing.T) {
	updates := []wikiUpdate{{Action: "create", Path: "프로젝트/a.md", Title: "A", Summary: "s", Content: "본문"}}

	without := buildCritiquePrompt(updates, "index", nil, nil)
	if strings.Contains(without, "미충족 수요") {
		t.Errorf("no demand must omit the demand section: %q", without)
	}

	with := buildCritiquePrompt(updates, "index", nil, []string{"완도", "징코"})
	if !strings.Contains(with, "미충족 수요") || !strings.Contains(with, "완도") {
		t.Errorf("demand section missing or incomplete: %q", with)
	}
	if !strings.Contains(with, `"drop"하지 마세요`) {
		t.Errorf("demand section must carry the don't-drop directive: %q", with)
	}
}
