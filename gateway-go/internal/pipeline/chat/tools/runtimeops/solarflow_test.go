package runtimeops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeSolarflowAction(t *testing.T) {
	cases := map[string]string{
		"search":       "search",
		"SEARCH":       "search",
		"검색":           "search",
		"재고":           "inventory",
		"마진분석":         "margin",
		"미수금":          "outstanding",
		"LC만기":         "lc_maturity",
		"수급전망":         "supply_forecast",
		"lc-fee":       "lc_fee",
		"price-trend":  "price_trend",
		"unknown-verb": "unknown-verb", // passthrough, later rejected
	}
	for in, want := range cases {
		if got := normalizeSolarflowAction(in); got != want {
			t.Errorf("normalizeSolarflowAction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolSolarflow_SearchRequiresQuery(t *testing.T) {
	fn := ToolSolarflow()
	out, err := fn(context.Background(), json.RawMessage(`{"action":"search"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "query") {
		t.Errorf("expected query guidance, got %q", out)
	}
}

func TestToolSolarflow_UnknownActionErrors(t *testing.T) {
	fn := ToolSolarflow()
	if _, err := fn(context.Background(), json.RawMessage(`{"action":"launch_rocket"}`)); err == nil {
		t.Fatal("expected error for unknown action")
	}
}
