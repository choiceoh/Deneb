package mailanalysis

import "testing"

const approvalFactsSource = `발주 품의서
공급사: 진코솔라
품목: 태양광 모듈 640W, 수량 2,000장, 단가 145원/W, 금액 290,000,000원
케이블 GV 26SQ, 수량 207m, 단가 25,600원/m, 금액 5,299,200원
합계(공급가액): 295,299,200원 (부가세 별도)`

func TestVerifyApprovalCostFacts_KeepsVerbatimRows(t *testing.T) {
	facts := &ApprovalCostFacts{
		IsCost:       true,
		Counterparty: "진코솔라",
		DocKind:      "발주품의",
		Amount: QuotedFact{
			Value: "295,299,200원",
			Quote: "합계(공급가액): 295,299,200원 (부가세 별도)",
		},
		LineItems: []ApprovalCostLine{
			{
				Name: "태양광 모듈 640W", Qty: "2,000장", UnitPrice: "145원/W", Amount: "290,000,000원",
				Quote: "품목: 태양광 모듈 640W, 수량 2,000장, 단가 145원/W, 금액 290,000,000원",
			},
			{
				Name: "케이블 GV 26SQ", Qty: "207m", UnitPrice: "25,600원/m", Amount: "5,299,200원",
				Quote: "케이블 GV 26SQ, 수량 207m, 단가 25,600원/m, 금액 5,299,200원",
			},
		},
	}
	got := verifyApprovalCostFacts(facts, approvalFactsSource, nil)
	if got == nil {
		t.Fatal("expected facts to survive")
	}
	if got.Amount.Value != "295,299,200원" {
		t.Errorf("amount = %q", got.Amount.Value)
	}
	if len(got.LineItems) != 2 {
		t.Fatalf("line items = %d, want 2", len(got.LineItems))
	}
}

func TestVerifyApprovalCostFacts_DropsFabricatedRows(t *testing.T) {
	facts := &ApprovalCostFacts{
		IsCost: true,
		Amount: QuotedFact{Value: "999,999원", Quote: "합계(공급가액): 295,299,200원 (부가세 별도)"},
		LineItems: []ApprovalCostLine{
			// Unit price digits not in its quote → drop.
			{Name: "태양광 모듈 640W", UnitPrice: "150원/W", Quote: "품목: 태양광 모듈 640W, 수량 2,000장, 단가 145원/W, 금액 290,000,000원"},
			// Quote not in source → drop.
			{Name: "인버터", UnitPrice: "1,000원/W", Quote: "인버터 단가 1,000원/W"},
		},
	}
	if got := verifyApprovalCostFacts(facts, approvalFactsSource, nil); got != nil {
		t.Errorf("expected nil (all figures fabricated), got %+v", got)
	}
}

func TestVerifyApprovalCostFacts_NonCostDocumentIsNil(t *testing.T) {
	facts := &ApprovalCostFacts{IsCost: false, Counterparty: "인사팀"}
	if got := verifyApprovalCostFacts(facts, "연차 휴가 신청의 건", nil); got != nil {
		t.Errorf("expected nil for non-cost document, got %+v", got)
	}
}

func TestVerifyApprovalCostFacts_ExpenseOnlyNeedsAmount(t *testing.T) {
	source := "지출결의서\n6월 법인차량 주유비 480,000원 (부가세 별도)"
	facts := &ApprovalCostFacts{
		IsCost:      true,
		DocKind:     "지출결의",
		ExpenseKind: "주유비",
		Amount:      QuotedFact{Value: "480,000원", Quote: "6월 법인차량 주유비 480,000원 (부가세 별도)"},
	}
	got := verifyApprovalCostFacts(facts, source, nil)
	if got == nil || got.ExpenseKind != "주유비" || got.Amount.Value != "480,000원" {
		t.Fatalf("expense facts = %+v", got)
	}
}
