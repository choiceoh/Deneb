package marketapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestMarketMethodsReturnsQuoteSummaryWithComputedChangePct(t *testing.T) {
	methods := MarketMethods(MarketDeps{Fetch: func(context.Context) ([]market.Quote, int64, bool, error) {
		return []market.Quote{{Symbol: "KRW=X", Label: "원/달러", Price: 110, PrevClose: 100, Currency: "KRW"}}, 1234, true, nil
	}})
	resp := methods["miniapp.market.summary"](marketAuthContext(), marketRequest(t))
	var got MarketSummary
	decodeMarketResponse(t, resp, &got)
	if got.AsOf != 1234 || !got.Stale || len(got.Quotes) != 1 || got.Quotes[0].ChangePct != 10 {
		t.Fatalf("payload = %+v", got)
	}
}

func TestMarketMethodsAuthAndFailureBoundaries(t *testing.T) {
	if got := MarketMethods(MarketDeps{}); got != nil {
		t.Fatalf("nil fetch registered methods: %v", got)
	}
	h := MarketMethods(MarketDeps{Fetch: func(context.Context) ([]market.Quote, int64, bool, error) {
		return nil, 0, false, errors.New("upstream down")
	}})["miniapp.market.summary"]
	unauthorized := h(context.Background(), marketRequest(t))
	if unauthorized.OK || unauthorized.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("unauthorized response = %+v", unauthorized)
	}
	failed := h(marketAuthContext(), marketRequest(t))
	if failed.OK || failed.Error.Code != protocol.ErrDependencyFailed {
		t.Fatalf("dependency response = %+v", failed)
	}
}

func marketAuthContext() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{User: &clientauth.User{ID: 42}})
}

func marketRequest(t *testing.T) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("test", "miniapp.market.summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func decodeMarketResponse(t *testing.T, resp *protocol.ResponseFrame, out any) {
	t.Helper()
	if resp == nil || !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		t.Fatal(err)
	}
}
