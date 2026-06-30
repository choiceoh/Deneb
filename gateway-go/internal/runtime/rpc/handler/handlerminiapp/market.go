// market.go — miniapp.market.summary RPC handler.
//
// Serves a small fixed set of market quotes (원/달러, 코스피, WTI 유가, 구리) for the
// Andromeda 오늘 dashboard's 시장 card. Data comes from the market domain cache
// (Yahoo Finance, 10m TTL). Read-only; UNAVAILABLE when no fetcher is wired.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MarketDeps holds the cache fetcher. nil Fetch → handler not registered.
type MarketDeps struct {
	Fetch func(ctx context.Context) (quotes []market.Quote, asOf int64, stale bool, err error)
}

// MarketMethods returns the miniapp.market.* handler map, or nil when no fetcher
// is provided so method_registry can register conditionally.
func MarketMethods(deps MarketDeps) map[string]rpcutil.HandlerFunc {
	if deps.Fetch == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.market.summary": marketSummary(deps),
	}
}

// MarketQuote is one instrument on the wire. changePct is the percent change vs
// the previous close (0 when prevClose is unknown).
//
//deneb:wire
type MarketQuote struct {
	Symbol    string  `json:"symbol"`
	Label     string  `json:"label"`
	Price     float64 `json:"price"`
	PrevClose float64 `json:"prevClose"`
	ChangePct float64 `json:"changePct"`
	Currency  string  `json:"currency"`
}

// MarketSummary is the miniapp.market.summary response. asOf is epoch millis of
// the snapshot; stale is true when a live refresh failed and the last good
// snapshot is being served.
//
//deneb:wire
type MarketSummary struct {
	Quotes []MarketQuote `json:"quotes"`
	AsOf   int64         `json:"asOf"`
	Stale  bool          `json:"stale"`
}

func marketSummary(deps MarketDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		quotes, asOf, stale, err := deps.Fetch(ctx)
		if err != nil {
			return rpcerr.WrapDependencyFailed("fetch market data", err).Response(req.ID)
		}
		rows := make([]MarketQuote, 0, len(quotes))
		for _, q := range quotes {
			var changePct float64
			if q.PrevClose != 0 {
				changePct = (q.Price - q.PrevClose) / q.PrevClose * 100
			}
			rows = append(rows, MarketQuote{
				Symbol:    q.Symbol,
				Label:     q.Label,
				Price:     q.Price,
				PrevClose: q.PrevClose,
				ChangePct: changePct,
				Currency:  q.Currency,
			})
		}
		return rpcutil.RespondOK(req.ID, MarketSummary{Quotes: rows, AsOf: asOf, Stale: stale})
	}
}
