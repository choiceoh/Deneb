package routine

import "testing"

func TestFormatGroupedInt(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// The 2026-07-07 letter garbled usd_krw 1530.98 into "1,331원" when the
		// LLM reformatted the raw float itself — this bare grouped number is
		// what the relay injects for the model's digit-free token instead
		// (units stay in the model's prose, so they can never double up).
		{1530.98389, "1,531"},
		{1751.0586982554423, "1,751"},
		{999.4, "999"},
		{1000000, "1,000,000"},
	}
	for _, c := range cases {
		if got := formatGroupedInt(c.in); got != c.want {
			t.Errorf("formatGroupedInt(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseYahooCopperDisplay(t *testing.T) {
	body := []byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":6.2533,"currency":"USD","regularMarketTime":1783381570}}],"error":null}}`)
	got := parseYahooCopper(body)
	if !got.OK {
		t.Fatalf("parse failed: %+v", got)
	}
	// 6.2533 USD/lb * 2204.6226 lb/t = 13786.17...
	if got.Display != "13,786" {
		t.Errorf("Display = %q, want 13,786", got.Display)
	}
	if got.Token == "" {
		t.Error("Token missing — the letter model needs the placeholder to place")
	}
	if got.PricePerTon < 13786 || got.PricePerTon > 13787 {
		t.Errorf("PricePerTon = %v, want ~13786", got.PricePerTon)
	}
}
