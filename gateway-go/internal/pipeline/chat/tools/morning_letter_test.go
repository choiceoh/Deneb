package tools

import (
	"strings"
	"testing"
)

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

func TestGroupThousands(t *testing.T) {
	cases := map[string]string{
		"1":       "1",
		"999":     "999",
		"1000":    "1,000",
		"13786":   "13,786",
		"1234567": "1,234,567",
	}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Errorf("groupThousands(%q) = %q, want %q", in, got, want)
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

func TestFormatQuotePriceGrouping(t *testing.T) {
	if got := formatQuotePrice(1530.98389); got != "1,530.98" {
		t.Errorf("formatQuotePrice = %q, want 1,530.98", got)
	}
	if got := formatQuotePrice(-1234.5); got != "-1,234.50" {
		t.Errorf("formatQuotePrice = %q, want -1,234.50", got)
	}
	if !strings.Contains(formatQuotePrice(13786.17), "13,786") {
		t.Errorf("formatQuotePrice lost grouping: %q", formatQuotePrice(13786.17))
	}
}
