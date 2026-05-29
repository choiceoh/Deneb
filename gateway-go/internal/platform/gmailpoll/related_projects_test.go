package gmailpoll

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRelatedProjects(t *testing.T) {
	cands := []ProjectCandidate{
		{Path: "프로젝트/topsolar.md", Title: "TopSolar"},
		{Path: "프로젝트/deneb.md", Title: "Deneb"},
	}
	tests := []struct {
		name      string
		text      string
		wantPaths []string
		gone      string // substring that must NOT remain in clean text
	}{
		{
			name:      "valid tag parsed and stripped",
			text:      "본문 분석 내용.\nRELATED_PROJECTS: 프로젝트/topsolar.md",
			wantPaths: []string{"프로젝트/topsolar.md"},
			gone:      "RELATED_PROJECTS",
		},
		{
			name:      "multiple comma-separated",
			text:      "본문\nRELATED_PROJECTS: 프로젝트/topsolar.md, 프로젝트/deneb.md",
			wantPaths: []string{"프로젝트/topsolar.md", "프로젝트/deneb.md"},
			gone:      "RELATED_PROJECTS",
		},
		{
			name:      "hallucinated path dropped",
			text:      "본문\nRELATED_PROJECTS: 프로젝트/없는것.md, 프로젝트/deneb.md",
			wantPaths: []string{"프로젝트/deneb.md"},
		},
		{
			name:      "markdown bold marker tolerated",
			text:      "본문\n**RELATED_PROJECTS:** 프로젝트/topsolar.md",
			wantPaths: []string{"프로젝트/topsolar.md"},
			gone:      "RELATED_PROJECTS",
		},
		{
			name:      "duplicate paths de-duplicated",
			text:      "본문\nRELATED_PROJECTS: 프로젝트/deneb.md, 프로젝트/deneb.md",
			wantPaths: []string{"프로젝트/deneb.md"},
		},
		{
			name:      "no tag leaves text and yields no paths",
			text:      "본문 그냥 분석. 프로젝트 언급 없음.",
			wantPaths: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clean, paths := parseRelatedProjects(tc.text, cands)
			if !reflect.DeepEqual(paths, tc.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tc.wantPaths)
			}
			if !strings.Contains(clean, "본문") {
				t.Errorf("clean dropped the body text: %q", clean)
			}
			if tc.gone != "" && strings.Contains(clean, tc.gone) {
				t.Errorf("clean still contains stripped tag %q: %q", tc.gone, clean)
			}
		})
	}
}

func TestParseRelatedProjects_NoCandidates(t *testing.T) {
	text := "본문\nRELATED_PROJECTS: 프로젝트/x.md"
	clean, paths := parseRelatedProjects(text, nil)
	if paths != nil {
		t.Errorf("paths = %v, want nil when no candidates offered", paths)
	}
	// Without candidates there's nothing to validate against, so the text is
	// returned untouched (the tag, if any, stays — but no candidates means we
	// never injected the instruction that would produce one).
	if clean != text {
		t.Errorf("text mutated with no candidates: %q", clean)
	}
}

func TestProjectSelectionSuffix(t *testing.T) {
	if s := projectSelectionSuffix(nil); s != "" {
		t.Errorf("no candidates → want empty suffix, got %q", s)
	}
	s := projectSelectionSuffix([]ProjectCandidate{
		{Path: "프로젝트/a.md", Title: "프로젝트 A", Summary: "요약 텍스트"},
	})
	for _, want := range []string{"프로젝트/a.md", "프로젝트 A", "요약 텍스트", "RELATED_PROJECTS"} {
		if !strings.Contains(s, want) {
			t.Errorf("suffix missing %q:\n%s", want, s)
		}
	}
}

func TestProjectCandidates_NilProvider(t *testing.T) {
	var d PipelineDeps // ProjectsFn nil
	if got := d.projectCandidates(); got != nil {
		t.Errorf("nil ProjectsFn → want nil, got %v", got)
	}
}

func TestProjectCandidates_Capped(t *testing.T) {
	big := make([]ProjectCandidate, maxProjectCandidates+10)
	for i := range big {
		big[i] = ProjectCandidate{Path: "프로젝트/p.md"}
	}
	d := PipelineDeps{ProjectsFn: func() []ProjectCandidate { return big }}
	if got := len(d.projectCandidates()); got != maxProjectCandidates {
		t.Errorf("candidate count = %d, want cap %d", got, maxProjectCandidates)
	}
}
