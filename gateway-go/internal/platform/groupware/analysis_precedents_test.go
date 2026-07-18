package groupware

import (
	"strings"
	"testing"
	"time"
)

func recAt(doc, title, drafter string, age time.Duration) *ApprovalAnalysisRecord {
	return &ApprovalAnalysisRecord{
		DocID:      doc,
		Title:      title,
		Drafter:    drafter,
		Date:       "2026-07-01",
		Analysis:   "**요지** — " + title + " 승인 권고",
		Importance: "routine",
		CreatedAt:  time.Now().Add(-age),
	}
}

func TestRecentOrdersNewestFirstAcrossVersions(t *testing.T) {
	dir := t.TempDir()
	store := NewApprovalAnalysisStore(dir)
	old := recAt("d1", "금호타이어 인버터 발주", "김세미", 48*time.Hour)
	old.PromptVersion = "v1" // stale version must still count as a precedent
	fresh := recAt("d2", "밀양 자재 발주", "정현도", time.Hour)
	for _, r := range []*ApprovalAnalysisRecord{old, fresh} {
		if err := store.Save(r); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	got := store.Recent(10)
	if len(got) != 2 {
		t.Fatalf("Recent = %d records, want 2", len(got))
	}
	if got[0].DocID != "d2" || got[1].DocID != "d1" {
		t.Errorf("order = %s,%s, want newest first d2,d1", got[0].DocID, got[1].DocID)
	}
}

func TestSelectApprovalPrecedents(t *testing.T) {
	records := []*ApprovalAnalysisRecord{
		recAt("d1", "금호타이어 곡성공장 인버터 발주의 건", "김세미", 72*time.Hour),
		recAt("d2", "영광 신하리 다과비 지출 품의", "김승리", 24*time.Hour),
		recAt("d3", "금호타이어 곡성공장 케이블 구매 요청", "김세미", 12*time.Hour),
		recAt("self", "금호타이어 곡성공장 인버터 추가 발주", "김세미", time.Hour),
	}

	got := SelectApprovalPrecedents(records, "self", "금호타이어 곡성공장 인버터 추가 발주", "기안 김세미", 5)
	if len(got) != 2 {
		t.Fatalf("precedents = %d, want 2 (금호 2건, 영광 제외, self 제외)", len(got))
	}
	// d1 shares 3 tokens (금호타이어·곡성공장·인버터) + drafter, d3 shares 2 + drafter.
	if got[0].DocID != "d1" || got[1].DocID != "d3" {
		t.Errorf("order = %s,%s, want score-desc d1,d3", got[0].DocID, got[1].DocID)
	}

	if SelectApprovalPrecedents(records, "x", "완전히 무관한 제목", "", 5) != nil {
		t.Error("unrelated title must select nothing")
	}
}

func TestFormatApprovalPrecedentsCarriesGist(t *testing.T) {
	out := FormatApprovalPrecedents([]*ApprovalAnalysisRecord{
		recAt("d1", "금호타이어 인버터 발주", "김세미", time.Hour),
	})
	for _, want := range []string{"2026-07-01", "금호타이어 인버터 발주", "기안 김세미", "routine", "승인 권고"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted precedent missing %q in %q", want, out)
		}
	}
}
