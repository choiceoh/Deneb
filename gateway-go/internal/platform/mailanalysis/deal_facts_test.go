package mailanalysis

import "testing"

// dealFactsSource is a realistic 견적서-style source (analysis + verbatim
// attachment) the quotes must verify against.
const dealFactsSource = `견적 내용을 안내드립니다.
1. 설비용량: 2,940.5kW (모듈 545W × 5,395장)
2. 모듈 단가는 148원/W로 산정하였습니다.
3. 대금 지급: 선수금 10%, 중도금 40%, 잔금 50%
4. 하자보증기간은 준공 후 3년입니다.
지체상금은 1일당 계약금액의 1/1000로 합니다.`

func TestVerifyDealFacts_KeepsVerbatimQuotes(t *testing.T) {
	facts := &DealFacts{
		CapacityMW:   QuotedFact{Value: "2,940.5kW", Quote: "설비용량: 2,940.5kW (모듈 545W × 5,395장)"},
		UnitPrice:    QuotedFact{Value: "148원/W", Quote: "모듈 단가는 148원/W로 산정하였습니다."},
		PaymentTerms: QuotedFact{Value: "선수금 10% 중도금 40% 잔금 50%", Quote: "대금 지급: 선수금 10%, 중도금 40%, 잔금 50%"},
		Warranty:     QuotedFact{Value: "준공 후 3년", Quote: "하자보증기간은 준공 후 3년입니다."},
		DelayPenalty: QuotedFact{Value: "1일당 1/1000", Quote: "지체상금은 1일당 계약금액의 1/1000로 합니다."},
	}
	got := verifyDealFacts(facts, dealFactsSource, nil)
	if got == nil {
		t.Fatal("all-verbatim facts must survive, got nil")
	}
	for name, v := range map[string]string{
		"capacityMW": got.CapacityMW.Value, "unitPrice": got.UnitPrice.Value,
		"paymentTerms": got.PaymentTerms.Value, "warranty": got.Warranty.Value,
		"delayPenalty": got.DelayPenalty.Value,
	} {
		if v == "" {
			t.Errorf("%s dropped despite verbatim quote", name)
		}
	}
}

func TestVerifyDealFacts_DropsBadQuotes(t *testing.T) {
	cases := []struct {
		name string
		fact QuotedFact
	}{
		// Paraphrased quote — not verbatim in the source.
		{"paraphrase", QuotedFact{Value: "148원/W", Quote: "단가는 와트당 148원입니다"}},
		// Value carries a digit its own quote doesn't show (합산/추정).
		{"digit mismatch", QuotedFact{Value: "150원/W", Quote: "모듈 단가는 148원/W로 산정하였습니다."}},
		// Value without any quote.
		{"no quote", QuotedFact{Value: "148원/W", Quote: ""}},
	}
	for _, tc := range cases {
		got := verifyDealFacts(&DealFacts{UnitPrice: tc.fact}, dealFactsSource, nil)
		if got != nil {
			t.Errorf("%s: want nil (field dropped → empty set), got %+v", tc.name, got)
		}
	}
}

func TestVerifyDealFacts_WhitespaceAndCommaInsensitive(t *testing.T) {
	// LLM re-spaced the quote and dropped the comma from the value — both must
	// still verify (OCR/LLM re-spacing is normal, commas are display-only).
	facts := &DealFacts{
		CapacityMW: QuotedFact{Value: "2940.5kW", Quote: "설비용량:2,940.5kW(모듈 545W×5,395장)"},
	}
	got := verifyDealFacts(facts, dealFactsSource, nil)
	if got == nil || got.CapacityMW.Value == "" {
		t.Fatalf("whitespace/comma variance must not drop a verbatim quote, got %+v", got)
	}
}

func TestVerifyDealFacts_EmptyValueZeroesQuote(t *testing.T) {
	facts := &DealFacts{
		UnitPrice: QuotedFact{Value: "", Quote: "모듈 단가는 148원/W로 산정하였습니다."},
		Warranty:  QuotedFact{Value: "준공 후 3년", Quote: "하자보증기간은 준공 후 3년입니다."},
	}
	got := verifyDealFacts(facts, dealFactsSource, nil)
	if got == nil {
		t.Fatal("warranty should survive")
	}
	if got.UnitPrice.Quote != "" {
		t.Errorf("empty value must zero its dangling quote, got %q", got.UnitPrice.Quote)
	}
}

func TestDealFactsEmpty(t *testing.T) {
	if !(&DealFacts{}).Empty() {
		t.Error("zero DealFacts must be Empty")
	}
	var nilFacts *DealFacts
	if !nilFacts.Empty() {
		t.Error("nil DealFacts must be Empty")
	}
	if (&DealFacts{Warranty: QuotedFact{Value: "3년"}}).Empty() {
		t.Error("filled field must not be Empty")
	}
}

func TestDigitsCoveredBy(t *testing.T) {
	cases := []struct {
		value, quote string
		want         bool
	}{
		{"148원/W", "단가 148원/W", true},
		{"2,940.5kW", "2940.5kW", true}, // comma-normalized both sides
		{"1일당 1/1000", "지체상금은 1일당 계약금액의 1/1000로", true},
		{"150원/W", "단가 148원/W", false}, // digit not in quote
		{"협의 중", "단가는 협의 중입니다", true},  // digit-free value passes
	}
	for _, tc := range cases {
		if got := digitsCoveredBy(tc.value, tc.quote); got != tc.want {
			t.Errorf("digitsCoveredBy(%q, %q) = %v, want %v", tc.value, tc.quote, got, tc.want)
		}
	}
}
