package groupware

import (
	"testing"
	"time"
)

func TestApprovalAnalysisStore_RoundTripAndVersionSkew(t *testing.T) {
	dir := t.TempDir()
	store := NewApprovalAnalysisStore(dir)
	rec := &ApprovalAnalysisRecord{
		DocID:         "99178",
		Title:         "구매 품의",
		Analysis:      "요지: 테스트",
		Importance:    "attention",
		DurationMs:    12,
		PromptVersion: ApprovalAnalysisPromptVersion,
		CreatedAt:     time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("99178")
	if err != nil || got == nil {
		t.Fatalf("load = %+v err=%v", got, err)
	}
	if got.Analysis != rec.Analysis || got.Importance != "attention" {
		t.Fatalf("got %+v", got)
	}

	// Version skew → miss
	skew := *rec
	skew.PromptVersion = "v0"
	if err := store.Save(&skew); err != nil {
		t.Fatal(err)
	}
	miss, err := store.Load("99178")
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Fatalf("v0 record should be treated as miss, got %+v", miss)
	}

	nilStore := NewApprovalAnalysisStore("")
	if err := nilStore.Save(rec); err != nil {
		t.Fatal(err)
	}
	if got, err := nilStore.Load("99178"); got != nil || err != nil {
		t.Fatalf("empty dir store should no-op, got=%v err=%v", got, err)
	}
}
