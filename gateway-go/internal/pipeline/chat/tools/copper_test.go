package tools

import (
	"math"
	"testing"
)

// TestParseYahooCopper verifies the USD/lb → USD/metric-ton conversion for the
// Yahoo HG=F (COMEX copper) quote and that a valid response is marked OK.
func TestParseYahooCopper(t *testing.T) {
	body := []byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":4.25,"currency":"USD","regularMarketTime":1782000000}}],"error":null}}`)

	got := parseYahooCopper(body)
	if !got.OK {
		t.Fatalf("expected OK, got error: %q", got.Error)
	}
	const poundsPerTon = 2204.6226
	want := 4.25 * poundsPerTon // ≈ 9369.65 USD/ton
	if math.Abs(got.PricePerTon-want) > 0.01 {
		t.Errorf("PricePerTon = %.2f, want %.2f (price/lb × %.4f)", got.PricePerTon, want, poundsPerTon)
	}
	if got.Date == "" {
		t.Error("expected Date derived from regularMarketTime")
	}
}

// TestParseYahooCopper_Failures covers the response shapes that must degrade to
// a non-OK copperData rather than a bogus price.
func TestParseYahooCopper_Failures(t *testing.T) {
	cases := map[string]string{
		"empty result": `{"chart":{"result":[],"error":null}}`,
		"chart error":  `{"chart":{"result":[],"error":{"code":"Not Found","description":"No data found"}}}`,
		"zero price":   `{"chart":{"result":[{"meta":{"regularMarketPrice":0,"currency":"USD"}}],"error":null}}`,
		"not json":     `<html>rate limited</html>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseYahooCopper([]byte(body))
			if got.OK {
				t.Errorf("expected non-OK for %q, got OK with PricePerTon=%.2f", name, got.PricePerTon)
			}
			if got.Error == "" {
				t.Errorf("expected an error message for %q", name)
			}
		})
	}
}
