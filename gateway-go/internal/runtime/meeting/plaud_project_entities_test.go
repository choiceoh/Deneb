package meeting

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestRankMentionedProjectsPrefersTitleHit(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지"},
		{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도 154kV 해저케이블"},
		{Path: "프로젝트/기아-화성/대표.md", Title: "기아 화성"},
	}
	got := RankMentionedProjects("비금 주간회의", "오늘 비금도 케이블 잔금 이야기", cands, 2)
	if len(got) == 0 || !strings.Contains(got[0].Path, "비금도") {
		t.Fatalf("got %#v", got)
	}
	// Unrelated project must not appear
	for _, g := range got {
		if strings.Contains(g.Path, "기아") {
			t.Fatalf("unrelated ranked: %#v", got)
		}
	}
}

func TestRankMentionedProjectsPrefixHitBeigeum(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도 154kV 해저케이블"},
		{Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지"},
	}
	got := RankMentionedProjects("비금 주간", "잔금 독촉", cands, 2)
	if len(got) != 1 || !strings.Contains(got[0].Path, "비금도") {
		t.Fatalf("prefix 비금→비금도: %#v", got)
	}
}

func TestRankMentionedProjectsRejectsWeakSolarStem(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지"},
	}
	// "솔라" alone must not rank 솔라빌리지 (half-coverage rule).
	if got := RankMentionedProjects("솔라 미팅", "일반 이야기", cands, 2); len(got) != 0 {
		t.Fatalf("weak stem must not match: %#v", got)
	}
}

func TestRankMentionedProjectsEmptyWithoutHit(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지"},
	}
	if got := RankMentionedProjects("점심 식사", "날씨 이야기만", cands, 3); len(got) != 0 {
		t.Fatalf("want empty, got %#v", got)
	}
}

func TestFormatProjectEntityBlock(t *testing.T) {
	got := FormatProjectEntityBlock([]ProjectEntityFacts{{
		Title:  "비금도",
		Client: "ZTT",
		Sites:  []string{"신안군 임자면"},
		People: []string{"오선택", "이시연"},
		Orgs:   []string{"남도에코"},
		Tags:   []string{"해저케이블", "mail-analysis"}, // mail-analysis skipped
	}})
	for _, want := range []string{"비금도", "ZTT", "임자면", "이시연", "남도에코", "해저케이블"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "mail-analysis") {
		t.Fatal("noise tag should be skipped")
	}
}
