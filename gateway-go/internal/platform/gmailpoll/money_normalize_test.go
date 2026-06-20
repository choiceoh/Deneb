package gmailpoll

import (
	"strings"
	"testing"
)

// --- normalizeMoneyToInt: deterministic Korean money → integer ---

func TestNormalizeMoneyToInt_Values(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		parsed bool
	}{
		// Plain + separators.
		{"5,000,000", 5_000_000, true},
		{"5000000", 5_000_000, true},
		{"5,000,000원", 5_000_000, true},
		// Currency adornments.
		{"₩5,000,000", 5_000_000, true},
		{"KRW 5,000,000", 5_000_000, true},
		{"5,000,000 원", 5_000_000, true},
		// Korean units.
		{"500만원", 5_000_000, true},
		{"5백만", 5_000_000, true},
		{"5천만", 50_000_000, true},
		{"5억", 500_000_000, true},
		// Composite units (the tricky case).
		{"1억2천만", 120_000_000, true},
		{"1억 2천만", 120_000_000, true},
		{"3억5천만원", 350_000_000, true},
		// Number + unit + tail.
		{"5만3000", 53_000, true},
		// Unparseable / ambiguous → parsed=false (over-block guard).
		{"", 0, false},
		{"약 오백만원", 0, false},       // hangul number words not supported → don't block
		{"5,000,000.50", 0, false}, // decimal KRW ambiguous
		{"미정", 0, false},
	}
	for _, c := range cases {
		got, ok := normalizeMoneyToInt(c.in)
		if ok != c.parsed {
			t.Errorf("normalizeMoneyToInt(%q) parsed=%v, want %v (got value %d)", c.in, ok, c.parsed, got)
			continue
		}
		if ok && got != c.want {
			t.Errorf("normalizeMoneyToInt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestNormalizeMoneyToInt_NotationEquivalence is the over-block-prevention
// invariant: figures written four different ways must all reduce to the SAME
// integer, so a correct amount is never blanked just because the source spelled
// it differently than the extractor did.
func TestNormalizeMoneyToInt_NotationEquivalence(t *testing.T) {
	forms := []string{"5,000,000", "500만원", "5백만", "₩5,000,000"}
	const want int64 = 5_000_000
	for _, f := range forms {
		got, ok := normalizeMoneyToInt(f)
		if !ok || got != want {
			t.Errorf("%q normalized to (%d, %v), want (%d, true)", f, got, ok, want)
		}
	}
}

// --- sourceMoneyValues: scanning the document text for money figures ---

func TestSourceMoneyValues_ScansMixedNotations(t *testing.T) {
	src := "견적 총액은 5,000,000원이며 부가세 별도. 계약금 500만원, 잔금은 1억2천만 규모."
	vals := sourceMoneyValues(src)
	for _, want := range []int64{5_000_000, 120_000_000} {
		if _, ok := vals[want]; !ok {
			t.Errorf("source scan missed %d in %q (got %v)", want, src, vals)
		}
	}
}

// --- amountFoundInSource: the gate's corroboration check ---

func TestAmountFoundInSource(t *testing.T) {
	src := "총 5,000,000원 (모듈 100장)"
	cases := []struct {
		amount string
		found  bool
		parsed bool
	}{
		{"5,000,000원", true, true},  // exact notation
		{"500만원", true, true},       // different notation, same value
		{"₩5,000,000", true, true},  // adorned, same value
		{"7,500,000원", false, true}, // hallucinated figure absent from source
		{"미정", false, false},        // unparseable extracted amount → don't block
		{"", false, false},          // empty → not gated
	}
	for _, c := range cases {
		found, parsed := amountFoundInSource(c.amount, src)
		if found != c.found || parsed != c.parsed {
			t.Errorf("amountFoundInSource(%q) = (found=%v, parsed=%v), want (%v, %v)",
				c.amount, found, parsed, c.found, c.parsed)
		}
	}
}

// --- gate behavior via dealInfoFromExtract ---

// TestDealAmountGate_NotationDiffKeepsAmount is the strongest over-block guard:
// the extractor wrote "5,000,000원" but the source document spelled it "500만원".
// Integer equivalence must corroborate it, so the amount is KEPT (a substring
// check would wrongly blank it).
func TestDealAmountGate_NotationDiffKeepsAmount(t *testing.T) {
	src := "가나에너지 발주서. 공급가액 500만원, 납기 2주."
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "가나에너지",
		DocType:      "발주서",
		Amount:       "5,000,000원",
	}, src, nil)
	if got == nil {
		t.Fatal("expected a deal, got nil")
	}
	if got.Amount != "5,000,000원" {
		t.Errorf("notation-different but equal amount was not kept: Amount=%q", got.Amount)
	}
	if got.Summary != "" {
		t.Errorf("corroborated amount should not be flagged, got Summary=%q", got.Summary)
	}
}

// TestDealAmountGate_HallucinationBlanked: an amount with no source match is
// blanked and flagged, while the rest of the deal is preserved.
func TestDealAmountGate_HallucinationBlanked(t *testing.T) {
	src := "마바솔라 견적서. 모듈 단가 위주, 총액 표기 없음. 참고용."
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "마바솔라",
		DocType:      "견적서",
		Amount:       "12,345,000원", // not present in src
		DueDate:      "2026-07-15",
		Items:        []string{"모듈"},
		Summary:      "7월 견적",
	}, src, nil)
	if got == nil {
		t.Fatal("deal should be preserved (not nil) when only the amount fails")
	}
	if got.Amount != "" {
		t.Errorf("hallucinated amount should be blanked, got %q", got.Amount)
	}
	// Deal preserved.
	if got.Counterparty != "마바솔라" || got.DocType != "견적서" || got.DueDate != "2026-07-15" {
		t.Errorf("non-amount fields must survive the gate: %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0] != "모듈" {
		t.Errorf("items must survive the gate: %+v", got.Items)
	}
	// Visible flag (not silent), carries the rejected figure.
	if !strings.Contains(got.Summary, "원문 대조 실패") || !strings.Contains(got.Summary, "12,345,000") {
		t.Errorf("mismatch must be flagged visibly with the figure, got Summary=%q", got.Summary)
	}
}

// TestDealAmountGate_AmbiguousParsePassesThrough: when the extracted amount
// can't be normalized (e.g. hangul number words), the over-block guard keeps it
// untouched rather than blanking a possibly-correct figure.
func TestDealAmountGate_AmbiguousParsePassesThrough(t *testing.T) {
	src := "가나에너지 계약. 금액은 협의 후 확정."
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "가나에너지",
		Amount:       "오백만원", // hangul words — unparseable
		Summary:      "협의 중",
	}, src, nil)
	if got == nil {
		t.Fatal("expected a deal, got nil")
	}
	if got.Amount != "오백만원" {
		t.Errorf("ambiguous amount should pass through unchanged, got %q", got.Amount)
	}
	if got.Summary != "협의 중" {
		t.Errorf("ambiguous parse must not add a mismatch flag, got Summary=%q", got.Summary)
	}
}

// TestDealAmountGate_EmptyAmountNotGated: a blank amount (prompt allows it) is
// left blank with no flag.
func TestDealAmountGate_EmptyAmountNotGated(t *testing.T) {
	got := dealInfoFromExtract(dealExtract{
		IsDeal:       true,
		Counterparty: "가나에너지",
		Amount:       "",
		Summary:      "일반 안내",
	}, "본문에 금액 없음", nil)
	if got == nil {
		t.Fatal("expected a deal, got nil")
	}
	if got.Amount != "" || got.Summary != "일반 안내" {
		t.Errorf("empty amount must not be gated/flagged: %+v", got)
	}
}
