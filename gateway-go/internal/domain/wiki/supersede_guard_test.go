package wiki

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestMarkSuperseded_RefusesLayoutSlotsAndRawData: superseded_by means "a newer
// version of this same topic" and is consumed as retirement (search ×0.15,
// counterparty anchor drop, auto-archive at 30 days). Layout slots and raw
// evidence are never a stale version of anything — both misuses below are live
// damage from 2026-07/08.
func TestMarkSuperseded_RefusesLayoutSlotsAndRawData(t *testing.T) {
	s, _ := newVerifyStore(t)
	rep := "프로젝트/pl2-ymn-epc-001/대표.md"
	logPage := "프로젝트/pl2-ymn-epc-001/로그.md"
	ledger := "프로젝트/거래/sunkean.md"
	mail := "프로젝트/nde-sun-cbl-001/메일분석/2026070917302539227723.md"
	for _, p := range []string{rep, logPage, ledger, mail} {
		writePageT(t, s, p, "페이지", "프로젝트", "본문")
	}

	// The rep page is checked against a DIFFERENT project's rep (self-supersession
	// is already a no-op).
	sibling := "프로젝트/pl2-hsd-epc-001/대표.md"
	writePageT(t, s, sibling, "후속 사업", "프로젝트", "승계 사업")
	for _, c := range []struct{ old, by string }{
		{logPage, rep}, {ledger, rep}, {mail, rep}, {rep, sibling},
	} {
		old := c.old
		if err := s.MarkSuperseded(old, c.by); err == nil {
			t.Errorf("MarkSuperseded accepted %s → %s", old, c.by)
		}
		page, _ := s.ReadPage(old)
		if page != nil && page.Meta.SupersededBy != "" {
			t.Errorf("%s got a superseded_by flag despite the refusal", old)
		}
	}

	// Ordinary curated pages still supersede.
	writePageT(t, s, "기타/구값.md", "구값", "기타", "옛 수치")
	writePageT(t, s, "기타/새값.md", "새값", "기타", "새 수치")
	if err := s.MarkSuperseded("기타/구값.md", "기타/새값.md"); err != nil {
		t.Errorf("ordinary supersession rejected: %v", err)
	}
}

// TestDetectStaleSuperseded_SkipsLayoutPagesCarryingABadFlag: pages that already
// carry a wrong flag must be repaired, not archived — archiving a live project
// 로그.md removes it from recall (0.3 × 0.15).
func TestDetectStaleSuperseded_SkipsLayoutPagesCarryingABadFlag(t *testing.T) {
	s, wd := newVerifyStore(t)
	wd.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	logPage := "프로젝트/pl2-ymn-epc-001/로그.md"
	stale := "기타/구값.md"
	for _, p := range []string{logPage, stale} {
		writePageT(t, s, p, "페이지", "기타", "본문")
	}
	// Stamp the flags directly: MarkSuperseded now refuses the bad one.
	for _, c := range []struct{ path, by string }{
		{logPage, "프로젝트/pl2-ymn-epc-001/대표.md"},
		{stale, "기타/새값.md"},
	} {
		if err := s.UpdatePage(c.path, func(cur *Page) (*Page, error) {
			cur.Meta.SupersededBy = c.by
			cur.Meta.Updated = "2026-07-01"
			return cur, nil
		}); err != nil {
			t.Fatalf("seed %s: %v", c.path, err)
		}
	}

	var paths []string
	for _, f := range wd.detectStaleSuperseded() {
		paths = append(paths, f.PageA)
	}
	joined := strings.Join(paths, ",")
	if strings.Contains(joined, logPage) {
		t.Errorf("live project log queued for archive: %v", paths)
	}
	if !strings.Contains(joined, stale) {
		t.Errorf("ordinary stale-superseded page not detected: %v", paths)
	}
}
