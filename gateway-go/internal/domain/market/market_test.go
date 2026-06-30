package market

import (
	"math"
	"testing"
)

// A trimmed real Yahoo chart response (^KS11) — only the meta fields parseChart reads.
const sampleKospi = `{"chart":{"result":[{"meta":{"currency":"KRW","symbol":"^KS11",
"regularMarketPrice":8642.85,"chartPreviousClose":8394.65,"regularMarketTime":1782795020}}],"error":null}}`

func TestParseChart_PriceAndPrevClose(t *testing.T) {
	q, err := parseChart([]byte(sampleKospi), "^KS11", "코스피", 1)
	if err != nil {
		t.Fatalf("parseChart: %v", err)
	}
	if q.Price != 8642.85 {
		t.Errorf("price = %v, want 8642.85", q.Price)
	}
	if q.PrevClose != 8394.65 {
		t.Errorf("prevClose = %v, want 8394.65", q.PrevClose)
	}
	if q.Currency != "KRW" {
		t.Errorf("currency = %q, want KRW", q.Currency)
	}
	if q.Label != "코스피" {
		t.Errorf("label = %q, want 코스피", q.Label)
	}
	if q.AsOf != 1782795020*1000 {
		t.Errorf("asOf = %d, want %d", q.AsOf, 1782795020*1000)
	}
}

// previousClose is the fallback when chartPreviousClose is absent.
func TestParseChart_PreviousCloseFallback(t *testing.T) {
	body := `{"chart":{"result":[{"meta":{"currency":"USD","regularMarketPrice":70.06,"previousClose":70.75}}]}}`
	q, err := parseChart([]byte(body), "CL=F", "WTI 유가", 1)
	if err != nil {
		t.Fatalf("parseChart: %v", err)
	}
	if q.PrevClose != 70.75 {
		t.Errorf("prevClose = %v, want 70.75 (previousClose fallback)", q.PrevClose)
	}
}

// scale converts the raw price into the display unit (copper USD/lb → USD/tonne).
func TestParseChart_Scale(t *testing.T) {
	body := `{"chart":{"result":[{"meta":{"currency":"USD","regularMarketPrice":4.50,"chartPreviousClose":4.40}}]}}`
	q, err := parseChart([]byte(body), "HG=F", "구리", poundsPerTonne)
	if err != nil {
		t.Fatalf("parseChart: %v", err)
	}
	approx := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
	if want := 4.50 * poundsPerTonne; !approx(q.Price, want) {
		t.Errorf("price = %v, want ~%v (USD/tonne)", q.Price, want)
	}
	if want := 4.40 * poundsPerTonne; !approx(q.PrevClose, want) {
		t.Errorf("prevClose = %v, want ~%v (USD/tonne)", q.PrevClose, want)
	}
}

func TestParseChart_Errors(t *testing.T) {
	cases := map[string]string{
		"empty result": `{"chart":{"result":[]}}`,
		"no price":     `{"chart":{"result":[{"meta":{"currency":"KRW"}}]}}`,
		"malformed":    `not json`,
	}
	for name, body := range cases {
		if _, err := parseChart([]byte(body), "X", "X", 1); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
