package observe

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// The production counters are intentionally different orders of magnitude:
// eight successful requests reused 249,600 tokens, not eight tokens.
func TestSTCacheUnitsFromScrapeThroughRates(t *testing.T) {
	body := engineMetricsBody(10, 1, 10, 1000, 20, 423319, 1000, 1, 2, 0, 0) + `
vllm:prefix_cache_queries_total{engine="st"} 130
vllm:prefix_cache_hits_total{engine="st"} 8
st:prefix_reused_tokens_total{engine="st"} 249600
st:prefix_reused_tokens_total_created{engine="st"} 999999
`
	srv := engineServer(t, body)
	c, ok := FetchEngineCounters(context.Background(), srv.URL+"/metrics")
	if !ok || !c.CacheTokensKnown || c.CachedPromptTokens != 249600 {
		t.Fatalf("scrape = %+v, %v", c, ok)
	}
	d, ok := EngineDeltaBetween(EngineCounters{CacheTokensKnown: true}, c)
	r := d.Rates()
	if !ok || !r.PromptCacheMeasured || math.Abs(r.PromptCacheHitRatio-249600.0/423319) > 1e-12 || r.CachedPromptTokens != 249600 || r.CachePromptTokens != 423319 {
		t.Fatalf("token rates = %+v, %v", r, ok)
	}
	if math.Abs(r.PrefixRequestHitRatio-8.0/130) > 1e-12 || r.PrefixHitRequests != 8 || r.PrefixLookupRequests != 130 {
		t.Fatalf("request rates = %+v", r)
	}
}

func TestCacheTokenPresenceZeroAndCounterReset(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprint(present), func(t *testing.T) {
			body := engineMetricsBody(0, 0, 0, 0, 0, 1000, 0, 0, 0, 0, 0)
			if present {
				body += "st:prefix_reused_tokens_total{engine=\"st\"} 0\n"
			}
			srv := engineServer(t, body)
			c, ok := FetchEngineCounters(context.Background(), srv.URL+"/metrics")
			if !ok || c.CacheTokensKnown != present {
				t.Fatalf("presence = %+v", c)
			}
			d, ok := EngineDeltaBetween(EngineCounters{CacheTokensKnown: present}, c)
			if !ok || d.Rates().PromptCacheMeasured != present {
				t.Fatalf("zero != missing: %+v", d.Rates())
			}
		})
	}
	a := EngineCounters{PromptTokens: 1000, CachedPromptTokens: 500, CacheTokensKnown: true}
	b := EngineCounters{PromptTokens: 2000, CachedPromptTokens: 100, CacheTokensKnown: true}
	if _, ok := EngineDeltaBetween(a, b); ok {
		t.Fatal("reuse counter reset must not produce a delta")
	}
	// A metric appearing/disappearing mid-window must rebaseline only cache tokens.
	a.CacheTokensKnown = false
	d, ok := EngineDeltaBetween(a, b)
	if !ok || d.PromptTokens != 1000 || d.CachePromptTokens != 0 {
		t.Fatalf("appearing metric: %+v", d)
	}
	a.CacheTokensKnown = true
	b.CacheTokensKnown = false
	d, ok = EngineDeltaBetween(a, b)
	if !ok || d.CachePromptTokens != 0 {
		t.Fatalf("missing metric: %+v", d)
	}
}
