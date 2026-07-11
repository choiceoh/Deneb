// market_tool.go — ad-hoc market quotes for the agent. The market.Cache
// (원/달러 · 코스피 · WTI · 구리, keyless Yahoo, 10m TTL) has powered the 오늘
// dashboard card since it was built, but the agent could only surface market
// data through the morning letter's own fetchers — "환율 지금 얼마?" mid-day
// had no tool. Same promote-internal-code pattern as weather.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// MarketSummaryFunc matches market.Cache.Summary — injected by the server so
// the agent tool and the miniapp dashboard share one cache (one upstream
// fetch, one consistent asOf).
type MarketSummaryFunc func(ctx context.Context) (quotes []market.Quote, asOf int64, stale bool, err error)

// ToolMarket returns the market tool. fetch nil is guarded at registration.
func ToolMarket(fetch MarketSummaryFunc) ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		quotes, asOf, stale, err := fetch(ctx)
		if err != nil {
			return "", fmt.Errorf("시세 조회 실패: %w", err)
		}
		if len(quotes) == 0 {
			return "시세 데이터가 없습니다 (업스트림 무응답).", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "시장 시세 (%s 기준", time.UnixMilli(asOf).Format("01-02 15:04"))
		if stale {
			b.WriteString(", 갱신 실패로 캐시 값")
		}
		b.WriteString("):\n")
		for _, q := range quotes {
			fmt.Fprintf(&b, "- %s: %s %s", q.Label, formatQuotePrice(q.Price), q.Currency)
			if q.PrevClose > 0 {
				pct := (q.Price - q.PrevClose) / q.PrevClose * 100
				fmt.Fprintf(&b, " (%+.2f%% vs 전일)", pct)
			}
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

// formatQuotePrice renders a price with thousands separators and a sensible
// decimal precision (indices/FX read naturally with 2 decimals).
func formatQuotePrice(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	dot := strings.IndexByte(s, '.')
	intPart, frac := s[:dot], s[dot:]
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	if neg {
		return "-" + textutil.GroupThousands(intPart) + frac
	}
	return textutil.GroupThousands(intPart) + frac
}
