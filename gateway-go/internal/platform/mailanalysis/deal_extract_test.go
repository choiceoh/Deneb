package mailanalysis

import "testing"

func TestDealInfoFromExtract_NotADeal(t *testing.T) {
	if got := dealInfoFromExtract(dealExtract{IsDeal: false, Counterparty: "마바솔라"}, "", nil); got != nil {
		t.Errorf("isDeal=false should yield nil, got %+v", got)
	}
}

func TestDealInfoFromExtract_EmptyCounterparty(t *testing.T) {
	if got := dealInfoFromExtract(dealExtract{IsDeal: true, Counterparty: "   "}, "", nil); got != nil {
		t.Errorf("blank counterparty should yield nil, got %+v", got)
	}
}

func TestDealInfoFromExtract_RejectsSelfCounterparty(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "topsolar.kr")
	t.Setenv("DENEB_MAIL_OUR_NAMES", "탑솔라")

	// A 견적서 our own firm issued, mis-extracted with our name as the
	// counterparty, must yield nil — no self-ledger (거래/탑솔라㈜.md) minted, and
	// with it the whole downstream chain (notebook pin, deal-question card) is
	// suppressed too.
	for _, self := range []string{"탑솔라㈜", "탑솔라(주)", "TOPSOLAR CO.,LTD"} {
		if got := dealInfoFromExtract(dealExtract{
			IsDeal:       true,
			Counterparty: self,
			DocType:      "견적서",
			Summary:      "6월 모듈 견적",
		}, "6월 모듈 견적", nil); got != nil {
			t.Errorf("self counterparty %q should yield nil, got %+v", self, got)
		}
	}

	// A real external counterparty in the same shape still files normally.
	if got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "무림피앤피",
		DocType:      "견적서",
	}, "", nil); got == nil || got.Counterparty != "무림피앤피" {
		t.Errorf("external counterparty must survive, got %+v", got)
	}
}

func TestDealInfoFromExtract_TrimsAndDropsEmptyItems(t *testing.T) {
	// Source carries the same 5,000,000 so the amount gate corroborates it and
	// the trimmed value survives.
	src := "견적 총액 5,000,000원 모듈 인버터 6월 견적"
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "  마바솔라  ",
		DocType:      " 견적서 ",
		Amount:       " 5,000,000원 ",
		DueDate:      "2026-06-30",
		Items:        []string{" 모듈 ", "", "   ", "인버터"},
		Summary:      " 6월 견적 ",
	}, src, nil)
	if got == nil {
		t.Fatal("expected a DealInfo, got nil")
	}
	if got.Counterparty != "마바솔라" || got.DocType != "견적서" || got.Amount != "5,000,000원" || got.Summary != "6월 견적" {
		t.Errorf("fields not trimmed: %+v", got)
	}
	if len(got.Items) != 2 || got.Items[0] != "모듈" || got.Items[1] != "인버터" {
		t.Errorf("items not cleaned: %+v", got.Items)
	}
}
