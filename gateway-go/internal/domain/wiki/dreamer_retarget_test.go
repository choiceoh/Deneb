package wiki

import (
	"io"
	"log/slog"
	"testing"
)

func newRetargetDreamer(t *testing.T) (*Store, *WikiDreamer) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, &WikiDreamer{store: s, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestRetargetDreamUpdate_RepProposalNeverLandsOnLogOrChild: a 대표페이지 proposal
// is not satisfied by the folder's 로그.md or a detail page. 2026-08-19 a rep
// update for a new project landed in another project's 로그.md; 2026-08-21 a rep
// re-creation landed on a child page, leaving the folder without a 대표.
func TestRetargetDreamUpdate_RepProposalNeverLandsOnLogOrChild(t *testing.T) {
	s, wd := newRetargetDreamer(t)
	writePageT(t, s, "프로젝트/pl2-kia-epc-002/기아-al광주-2공장-태양광-모듈-입찰.md",
		"기아 AL광주 2공장 태양광 모듈 입찰", "프로젝트", "자식 페이지 본문")

	u := wikiUpdate{
		Action: "update", Path: "프로젝트/pl2-kia-epc-002/대표.md",
		Title: "기아 AL광주 2공장 태양광", Category: "프로젝트", Content: "## 현재 상태",
	}
	got := wd.retargetDreamUpdate(u)
	if got.Path != u.Path {
		t.Errorf("rep proposal retargeted to %q, want the proposed path", got.Path)
	}
	if got.retargetedFrom != "" {
		t.Errorf("retargetedFrom set on a refused retarget: %q", got.retargetedFrom)
	}
}

// TestRetargetDreamUpdate_IgnoresWeakTitleMatch: the "title" stage is an FTS
// query over the whole page with an OR fallback and a floor a single shared
// token clears — that is how a scopolamine-warning proposal landed on a
// smart-glasses page (2026-07-30) and rewrote its identity.
func TestRetargetDreamUpdate_IgnoresWeakTitleMatch(t *testing.T) {
	s, wd := newRetargetDreamer(t)
	writePageT(t, s, "기타/even-realities-g2-smart-glasses.md",
		"Even Realities G2 스마트 안경", "기타",
		"번역·AI 탑재 안경. 데이팅 앱 사용자 후기와 콜롬비아 출시 일정도 다룬다.")

	u := wikiUpdate{
		Action: "create", Path: "기타/데이팅앱-스코폴라민-경고.md",
		Title:    "데이팅 앱 기반 범죄: 스코폴라민 및 콜롬비아 여행 경고",
		Category: "기타", Content: "미 대사관 경고",
	}
	got := wd.retargetDreamUpdate(u)
	if got.Path != u.Path {
		t.Errorf("weak title match retargeted to %q, want the proposed path", got.Path)
	}

	// A real title match still retargets (same subject, spelled differently).
	writePageT(t, s, "기타/영산고-태양광.md", "영산고 태양광", "기타", "본문")
	same := wikiUpdate{
		Action: "create", Path: "기타/영산고태양광.md",
		Title: "영산고 태양광 진행", Category: "기타", Content: "갱신",
	}
	if got := wd.retargetDreamUpdate(same); got.Path != "기타/영산고-태양광.md" {
		t.Errorf("same-subject proposal = %q, want the existing page", got.Path)
	}
}

// TestTitleTokenOverlap_IgnoresGenericWords: business pages all carry 대표/진행/
// 검토/관련 — sharing only those is not sharing a subject.
func TestTitleTokenOverlap_IgnoresGenericWords(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"기아 광주 진행 현황", "금호타이어 곡성 진행 현황", false},
		{"영산고 태양광", "영산고-태양광", true},
		{"기아 AL광주 2공장 태양광", "기아 AL광주 캐노피 견적", true},
		{"데이팅 앱 기반 범죄", "Even Realities G2 스마트 안경", false},
	}
	for _, c := range cases {
		if got := titleTokenOverlap(c.a, c.b); got != c.want {
			t.Errorf("titleTokenOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
