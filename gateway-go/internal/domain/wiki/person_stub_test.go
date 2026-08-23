package wiki

import (
	"testing"
	"time"
)

// TestIsPersonStubPage_ProseAndPIDLiftTheStubLabel: the demotion must catch the
// contacts-sync template (204 pages seeded in one 2026-07-27 sync, ~90% still
// prose-less) without touching a page someone actually wrote, and never touch a
// person the org chart tracks by pid.
func TestIsPersonStubPage_ProseAndPIDLiftTheStubLabel(t *testing.T) {
	const template = "# 강동민\n\n## 소속 · 직책\n\n_(미기재)_\n\n## 연락처\n\n- 이메일: kang@partner.co\n- 전화: 010-0000-0000\n\n## 변경 이력\n\n_주소록에서 동기화됨_\n"

	cases := []struct {
		name string
		path string
		page *Page
		want bool
	}{
		{"sync template", "인물/강동민.md", &Page{Body: template}, true},
		{"empty body", "인물/빈페이지.md", &Page{Body: ""}, true},
		{
			"one written sentence",
			"인물/백창선.md",
			&Page{Body: template + "\n완도 관산포 인수 실무를 총괄하고 계약 창구를 맡는다.\n"},
			false,
		},
		{
			"pid means the org chart tracks this person",
			"인물/오선택.md",
			&Page{Meta: Frontmatter{PID: "p-pl0-001"}, Body: template},
			false,
		},
		{"not a person page", "업무/화신일렉트릭.md", &Page{Body: ""}, false},
		{"nil page", "인물/x.md", nil, false},
	}
	for _, c := range cases {
		if got := isPersonStubPage(c.path, c.page); got != c.want {
			t.Errorf("%s: isPersonStubPage = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestValidityFactor_DemotesPersonStubsBelowCuratedPages: a stub keeps working
// for direct reads and name resolution but must not win an evidence slot from a
// page that says something.
func TestValidityFactor_DemotesPersonStubsBelowCuratedPages(t *testing.T) {
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	stub := &Page{Meta: Frontmatter{Updated: "2026-08-20"}, Body: "## 연락처\n\n- 이메일: a@b.co\n"}
	curated := &Page{Meta: Frontmatter{Updated: "2026-08-20"}, Body: "곡성 공장 태양광 계약 창구. 8월 견적 회신 담당."}

	stubF := validityFactor("인물/강동민.md", stub, now)
	curatedF := validityFactor("인물/백창선.md", curated, now)
	if stubF >= curatedF {
		t.Errorf("stub factor %v not below curated %v", stubF, curatedF)
	}
	// Still above archived (0.3): a stub is current, just empty.
	archived := validityFactor("인물/보관.md", &Page{
		Meta: Frontmatter{Archived: true, Updated: "2026-08-20"},
		Body: "곡성 공장 태양광 계약 창구. 8월 견적 회신 담당.",
	}, now)
	if stubF <= archived {
		t.Errorf("stub factor %v should outrank archived %v", stubF, archived)
	}
}
