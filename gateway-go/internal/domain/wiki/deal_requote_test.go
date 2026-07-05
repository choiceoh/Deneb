package wiki

import (
	"strings"
	"testing"
	"time"
)

func requoteInput(sourceRef, unitPrice, capacity, amount string) DealPageInput {
	in := DealPageInput{
		Counterparty: "남도에코",
		DocType:      "견적서",
		Amount:       amount,
		SourceRef:    sourceRef,
	}
	if unitPrice != "" || capacity != "" {
		in.Terms = &DealTerms{
			UnitPrice: QuotedTerm{Value: unitPrice, Quote: "단가: " + unitPrice},
			Capacity:  QuotedTerm{Value: capacity, Quote: "물량: " + capacity},
		}
	}
	return in
}

func TestDetectRequote_UnitPriceChange(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	if _, _, err := s.UpsertDealPage(requoteInput("mail:q1", "25,600원/m", "2,940.5kW", "3.2억"), now); err != nil {
		t.Fatal(err)
	}

	next := requoteInput("mail:q2", "24,800원/m", "2,940.5kW", "")
	change := s.DetectRequote(next, now.Add(24*time.Hour))
	if change == nil || len(change.Fields) != 1 {
		t.Fatalf("want exactly the 단가 change, got %+v", change)
	}
	f := change.Fields[0]
	if f.Label != "단가" || f.Old != "25,600원/m" || f.New != "24,800원/m" {
		t.Errorf("field = %+v", f)
	}
	if !f.HasPct || f.Pct > -3.1 || f.Pct < -3.2 {
		t.Errorf("pct = %v, want ≈ -3.125", f.Pct)
	}
	line := change.StatusLine()
	if !strings.Contains(line, "재견적(남도에코)") || !strings.Contains(line, "25,600원/m → 24,800원/m") {
		t.Errorf("status line = %q", line)
	}
}

func TestDetectRequote_NoFalsePositives(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	if _, _, err := s.UpsertDealPage(requoteInput("mail:q1", "25,600원/m", "", ""), now); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   DealPageInput
	}{
		// Same figures resent → not a change.
		{"identical", requoteInput("mail:q2", "25,600원/m", "", "")},
		// Different unit — 원/W must never diff against 원/m.
		{"unit mismatch", requoteInput("mail:q3", "148원/W", "", "")},
		// Not a 견적서.
		{"contract doc", func() DealPageInput {
			in := requoteInput("mail:q4", "24,000원/m", "", "")
			in.DocType = "계약서"
			return in
		}()},
		// Different counterparty.
		{"other counterparty", func() DealPageInput {
			in := requoteInput("mail:q5", "24,000원/m", "", "")
			in.Counterparty = "대한전선"
			return in
		}()},
		// Re-analysis of the SAME mail (SourceRef match) — no prior to diff.
		{"same sourceRef", requoteInput("mail:q1", "24,000원/m", "", "")},
	}
	for _, tc := range cases {
		if change := s.DetectRequote(tc.in, now.Add(time.Hour)); change != nil {
			t.Errorf("%s: want nil, got %+v", tc.name, change)
		}
	}
}

func TestDetectRequote_CapacityAndAmount(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	if _, _, err := s.UpsertDealPage(requoteInput("mail:q1", "", "2,940.5kW", "320,000,000원"), now); err != nil {
		t.Fatal(err)
	}
	change := s.DetectRequote(requoteInput("mail:q2", "", "3.5MW", "350,000,000원"), now.Add(time.Hour))
	if change == nil || len(change.Fields) != 2 {
		t.Fatalf("want 물량+금액 changes, got %+v", change)
	}
	if change.Fields[0].Label != "물량" || change.Fields[1].Label != "금액" {
		t.Errorf("labels = %s, %s", change.Fields[0].Label, change.Fields[1].Label)
	}
}

func TestParseUnitPrice(t *testing.T) {
	cases := []struct {
		in   string
		val  float64
		unit string
		ok   bool
	}{
		{"25,600원/m", 25600, "원/m", true},
		{"148원/W", 148, "원/w", true},
		{"148 원/W", 148, "원/w", true}, // spacing collapses into the unit key
		{"협의 중", 0, "", false},
		{"5천원/W", 0, "", false}, // spelled-out Korean amounts out of scope
		{"", 0, "", false},
	}
	for _, tc := range cases {
		v, u, ok := parseUnitPrice(tc.in)
		if ok != tc.ok || (ok && (v != tc.val || u != tc.unit)) {
			t.Errorf("parseUnitPrice(%q) = %v,%q,%v want %v,%q,%v", tc.in, v, u, ok, tc.val, tc.unit, tc.ok)
		}
	}
}
