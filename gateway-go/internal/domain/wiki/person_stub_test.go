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

// The contacts-sync skeleton — org/title/company value lines plus contact
// bullets — must classify as a stub: every line restates the address book.
// Counting the value lines as prose let 267 skeletons dodge the first purge
// cycle (2026-08-25: purged=12 instead of draining the 280 backlog).
func TestIsPersonStubPageTreatsSyncValueLinesAsTemplate(t *testing.T) {
	page := &Page{Meta: Frontmatter{Category: "인물"}, Body: `## 소속 · 직책

- **소속**: 탑솔라그룹
- **직급 · 직책**: —

## 담당 · 관계

_(미기재)_

## 연락처

- 전화: 010-0000-0000
- 회사: 탑솔라그룹

_주소록에서 동기화됨_
`}
	if !isPersonStubPage("인물/야샤.md", page) {
		t.Fatal("contacts-sync skeleton not classified as stub")
	}
	// One human sentence still lifts it out.
	page.Body += "\n금호타이어 곡성 담당, 계약 창구. 실사 일정을 조율한다.\n"
	if isPersonStubPage("인물/야샤.md", page) {
		t.Fatal("page with real prose misclassified as stub")
	}
}

// A page the operator judged must survive the stub purge: the decision is
// frontmatter (ack) or a blockquote callout, and the prose scan sees neither —
// so a curated page read as "contentless" and the purge deleted the answer.
func TestIsPersonStubPage_KeepsOperatorDecisions(t *testing.T) {
	acked := &Page{Meta: Frontmatter{Title: "김용범", IdentityReviewed: []string{"posco.com", "topsolar.kr"}}}
	acked.Body = "## 소속 · 직책\n- **소속**: 탑솔라(주)\n\n## 연락처\n- 이메일: ybby69@topsolar.kr\n\n_주소록에서 동기화됨_"
	if isPersonStubPage("인물/김용범.md", acked) {
		t.Error("확인 표시가 있는 페이지가 빈 껍데기로 판정됨")
	}

	noted := &Page{Meta: Frontmatter{Title: "김성환"}}
	noted.Body = "## 소속 · 직책\n- **소속**: 탑솔라\n\n> ⚠️ **동명이인 주의**: BM에너지 김성환은 별개 인물이다\n\n_주소록에서 동기화됨_"
	if isPersonStubPage("인물/김성환.md", noted) {
		t.Error("동명이인 주의가 있는 페이지가 빈 껍데기로 판정됨")
	}

	// The purge itself stays intact for genuinely empty seeds.
	bare := &Page{Meta: Frontmatter{Title: "홍길동"}}
	bare.Body = "## 소속 · 직책\n- **소속**: —\n\n## 담당 · 관계\n\n_(미기재)_\n\n_주소록에서 동기화됨_"
	if !isPersonStubPage("인물/홍길동.md", bare) {
		t.Error("빈 씨앗 페이지가 퍼지 대상에서 빠짐")
	}
}
