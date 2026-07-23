package wiki

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCleanProjectFolderName_NormalizesTrailingDebrisKeepsMidNameTokens pins
// mint-time name hygiene against the five real mail-subject-named folders the
// 2026-07-05 audit found. Only TRAILING debris is stripped (dates, 요청/송부-class
// suffixes, dangling dashes) — mid-name tokens stay, and business tail words
// (발주, 견적) are kept.
func TestCleanProjectFolderName_NormalizesTrailingDebrisKeepsMidNameTokens(t *testing.T) {
	cases := []struct{ in, want string }{
		{"강진-신다산-epc-계약서-법무검토-의견-(2026-06-30)", "강진-신다산-epc-계약서-법무검토"},
		{"부산항터미널-태양광-(신선대)-—-가배치-요청-(2026-06-25)", "부산항터미널-태양광-(신선대)-—-가배치"},
		{"제이티에너지-—-청도군-풍각면-화산리-턴키-견적-요청-(2026-06-22)", "제이티에너지-—-청도군-풍각면-화산리-턴키-견적"},
		{"현대자동차-전주공장-태양광-모듈-추가-발주-(2026-07-01)", "현대자동차-전주공장-태양광-모듈-추가-발주"},
		// 송부 sits mid-name — trailing-only stripping leaves it (manual rename).
		{"아르고에너지(argo-energy)-nda-송부-–-태양광-사업-전략적-협력-검토", "아르고에너지(argo-energy)-nda-송부-–-태양광-사업-전략적-협력-검토"},
		{"당진-솔라빌리지", "당진-솔라빌리지"},
		{"요청", "요청"}, // single segment never stripped to nothing
	}
	for _, tc := range cases {
		if got := cleanProjectFolderName(tc.in); got != tc.want {
			t.Errorf("cleanProjectFolderName(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

// TestCleanNewProjectRepPath_NormalizesNewMintKeepsExistingRoutesToCleanTwin:
// mint-time only — existing dirty folders keep their paths; a new dirty mint
// is cleaned; and when the CLEAN folder already exists the dirty twin routes
// into it.
func TestCleanNewProjectRepPath_NormalizesNewMintKeepsExistingRoutesToCleanTwin(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	dirty := RepPagePath("영산고-발주-요청-(2026-07-01)")
	clean := RepPagePath("영산고-발주")

	// New mint → cleaned path.
	if got := s.CleanNewProjectRepPath(dirty); got != clean {
		t.Errorf("new mint = %q, want %q", got, clean)
	}

	// Existing dirty folder → path stability, never renamed in place.
	mustWrite(t, s, dirty, &Page{Meta: Frontmatter{Title: "영산고 발주"}, Body: "본문."})
	if got := s.CleanNewProjectRepPath(dirty); got != dirty {
		t.Errorf("existing page must keep its path, got %q", got)
	}

	// Clean twin exists → dirty NEW path routes into it.
	dirty2 := RepPagePath("당진-솔라빌리지-송부-(2026-07-02)")
	mustWrite(t, s, RepPagePath("당진-솔라빌리지"), &Page{Meta: Frontmatter{Title: "당진 솔라빌리지"}, Body: "본문."})
	if got := s.CleanNewProjectRepPath(dirty2); got != RepPagePath("당진-솔라빌리지") {
		t.Errorf("dirty twin should route into the clean folder, got %q", got)
	}
}

// TestSetProjectStatus_DeduplicatesDuplicateStatusBullets: duplicate roll-up
// lines collapse — the audit found rep pages whose whole 현재 상태 was one
// no-information bullet twice.
func TestSetProjectStatus_DeduplicatesDuplicateStatusBullets(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	rel := RepPagePath("영광-bess")
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	if err := s.setProjectStatus(rel, []string{"BESS 구축 진행", "BESS 구축 진행", "계약금 입금 확인"}, "", now, ""); err != nil {
		t.Fatalf("setProjectStatus: %v", err)
	}
	page, err := s.ReadPage(rel)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	bullets := extractStatusBullets(page.Body)
	if len(bullets) != 2 {
		t.Fatalf("bullets = %v, want 2 (deduped)", bullets)
	}
	if strings.Count(page.Section("현재 상태"), "BESS 구축 진행") != 1 {
		t.Errorf("duplicate bullet survived:\n%s", page.Section("현재 상태"))
	}
}
