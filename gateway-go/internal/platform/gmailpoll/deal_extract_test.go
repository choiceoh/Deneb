package gmailpoll

import "testing"

func TestDealInfoFromExtract_NotADeal(t *testing.T) {
	if got := dealInfoFromExtract(dealExtract{IsDeal: false, Counterparty: "탑솔라"}); got != nil {
		t.Errorf("isDeal=false should yield nil, got %+v", got)
	}
}

func TestDealInfoFromExtract_EmptyCounterparty(t *testing.T) {
	if got := dealInfoFromExtract(dealExtract{IsDeal: true, Counterparty: "   "}); got != nil {
		t.Errorf("blank counterparty should yield nil, got %+v", got)
	}
}

func TestDealInfoFromExtract_TrimsAndDropsEmptyItems(t *testing.T) {
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "  탑솔라  ",
		DocType:      " 견적서 ",
		Amount:       " 5,000,000원 ",
		DueDate:      "2026-06-30",
		Items:        []string{" 모듈 ", "", "   ", "인버터"},
		Summary:      " 6월 견적 ",
	})
	if got == nil {
		t.Fatal("expected a DealInfo, got nil")
	}
	if got.Counterparty != "탑솔라" || got.DocType != "견적서" || got.Amount != "5,000,000원" || got.Summary != "6월 견적" {
		t.Errorf("fields not trimmed: %+v", got)
	}
	if len(got.Items) != 2 || got.Items[0] != "모듈" || got.Items[1] != "인버터" {
		t.Errorf("items not cleaned: %+v", got.Items)
	}
}
