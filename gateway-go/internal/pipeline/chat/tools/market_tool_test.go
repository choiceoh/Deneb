package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
)

func TestToolMarketReturnsFreshQuotes(t *testing.T) {
	asOf := time.Now().UnixMilli()
	fetch := func(context.Context) ([]market.Quote, int64, bool, error) {
		return []market.Quote{
			{Label: "원/달러", Currency: "KRW", Price: 1388.2, PrevClose: 1383.4, AsOf: asOf},
			{Label: "구리", Currency: "USD/t", Price: 9850, PrevClose: 9900, AsOf: asOf},
		}, asOf, false, nil
	}
	out, err := ToolMarket(fetch)(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("market: %v", err)
	}
	for _, want := range []string{"원/달러", "1,388.20", "+0.35%", "구리", "-0.51%"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "캐시 값") {
		t.Errorf("fresh fetch must not be marked stale: %q", out)
	}
}

func TestToolMarket_StaleAndError(t *testing.T) {
	stale := func(context.Context) ([]market.Quote, int64, bool, error) {
		return []market.Quote{{Label: "코스피", Currency: "KRW", Price: 2700, PrevClose: 2690}},
			time.Now().UnixMilli(), true, nil
	}
	out, err := ToolMarket(stale)(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if !strings.Contains(out, "캐시 값") {
		t.Errorf("stale serve must be labeled: %q", out)
	}

	fail := func(context.Context) ([]market.Quote, int64, bool, error) {
		return nil, 0, false, fmt.Errorf("upstream down")
	}
	if _, err := ToolMarket(fail)(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("fetch error must surface")
	}
}

func TestFormatQuotePriceGrouping(t *testing.T) {
	if got := formatQuotePrice(1530.98389); got != "1,530.98" {
		t.Errorf("formatQuotePrice = %q, want 1,530.98", got)
	}
	if got := formatQuotePrice(-1234.5); got != "-1,234.50" {
		t.Errorf("formatQuotePrice = %q, want -1,234.50", got)
	}
	if got := formatQuotePrice(13786.17); !strings.Contains(got, "13,786") {
		t.Errorf("formatQuotePrice lost grouping: %q", got)
	}
}
