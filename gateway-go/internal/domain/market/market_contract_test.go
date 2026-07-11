package market

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func chartBody(price, previous float64, currency string, marketTime int64) string {
	return `{"chart":{"result":[{"meta":{"regularMarketPrice":` + floatString(price) +
		`,"chartPreviousClose":` + floatString(previous) + `,"currency":"` + currency +
		`","regularMarketTime":` + intString(marketTime) + `}}]}}`
}

func floatString(v float64) string {
	return strings.TrimRight(strings.TrimRight(formatFloat(v), "0"), ".")
}

func formatFloat(v float64) string {
	// Fixtures only need small positive values with two decimal places.
	whole := int64(v)
	fraction := int64(math.Round((v - float64(whole)) * 100))
	if fraction == 0 {
		return intString(whole)
	}
	if fraction < 10 {
		return intString(whole) + ".0" + intString(fraction)
	}
	return intString(whole) + "." + intString(fraction)
}

func intString(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestParseChartFallbackScaleAndMetadataBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		scale      float64
		wantPrice  float64
		wantPrev   float64
		wantAsOf   int64
		wantCurr   string
		wantErrSub string
	}{
		{
			name:  "previous close fallback and zero scale",
			body:  `{"chart":{"result":[{"meta":{"regularMarketPrice":12.5,"previousClose":11.25,"currency":"USD","regularMarketTime":7}}]}}`,
			scale: 0, wantPrice: 12.5, wantPrev: 11.25, wantAsOf: 7000, wantCurr: "USD",
		},
		{
			name:  "negative scale becomes one",
			body:  `{"chart":{"result":[{"meta":{"regularMarketPrice":2,"chartPreviousClose":1}}]}}`,
			scale: -3, wantPrice: 2, wantPrev: 1,
		},
		{
			name:  "positive scale",
			body:  `{"chart":{"result":[{"meta":{"regularMarketPrice":2,"chartPreviousClose":1.5}}]}}`,
			scale: 10, wantPrice: 20, wantPrev: 15,
		},
		{name: "null result", body: `{"chart":{"result":null}}`, scale: 1, wantErrSub: "empty result"},
		{name: "missing chart", body: `{}`, scale: 1, wantErrSub: "empty result"},
		{name: "zero price", body: `{"chart":{"result":[{"meta":{"regularMarketPrice":0}}]}}`, scale: 1, wantErrSub: "no price"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quote, err := parseChart([]byte(tc.body), "SYM", "Label", tc.scale)
			if tc.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error = %v, want %q", err, tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChart: %v", err)
			}
			if quote.Symbol != "SYM" || quote.Label != "Label" || quote.Currency != tc.wantCurr ||
				quote.Price != tc.wantPrice || quote.PrevClose != tc.wantPrev || quote.AsOf != tc.wantAsOf {
				t.Fatalf("quote = %+v", quote)
			}
		})
	}
}

func TestFetchOneBuildsEscapedRequestAndValidatesResponse(t *testing.T) {
	var seenURL, seenAgent string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seenURL = req.URL.String()
		seenAgent = req.Header.Get("User-Agent")
		return response(http.StatusOK, chartBody(10.5, 9.5, "USD", 17)), nil
	})}
	quote, err := fetchOne(context.Background(), client, "A/B C", "Escaped", 2)
	if err != nil {
		t.Fatalf("fetchOne: %v", err)
	}
	if !strings.Contains(seenURL, "A%2FB%20C") || !strings.Contains(seenURL, "interval=1d") || !strings.Contains(seenURL, "range=1d") {
		t.Fatalf("request URL = %q", seenURL)
	}
	if seenAgent == "" || !strings.Contains(seenAgent, "Deneb") {
		t.Fatalf("User-Agent = %q", seenAgent)
	}
	if quote.Price != 21 || quote.PrevClose != 19 || quote.AsOf != 17000 {
		t.Fatalf("quote = %+v", quote)
	}

	for _, tc := range []struct {
		name string
		rt   roundTripFunc
		want string
	}{
		{name: "transport", rt: func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }, want: "offline"},
		{name: "status", rt: func(*http.Request) (*http.Response, error) {
			return response(http.StatusTooManyRequests, "limited"), nil
		}, want: "HTTP 429"},
		{name: "malformed", rt: func(*http.Request) (*http.Response, error) { return response(http.StatusOK, "{"), nil }, want: "unexpected end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fetchOne(context.Background(), &http.Client{Transport: tc.rt}, "X", "X", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFetchAllAllowsPartialAndErrorsWhenAllFail(t *testing.T) {
	var mu sync.Mutex
	requests := make([]string, 0, len(catalog))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, req.URL.Path)
		mu.Unlock()
		if strings.Contains(req.URL.Path, "KRW=X") || strings.Contains(req.URL.Path, "HG=F") {
			return response(http.StatusOK, chartBody(2, 1, "USD", 1)), nil
		}
		return response(http.StatusBadGateway, "down"), nil
	})}
	quotes, err := fetchAll(context.Background(), client)
	if err != nil {
		t.Fatalf("fetchAll partial: %v", err)
	}
	if len(quotes) != 2 || len(requests) != len(catalog) {
		t.Fatalf("quotes=%+v requests=%+v", quotes, requests)
	}
	if quotes[0].Symbol != "KRW=X" || quotes[1].Symbol != "HG=F" {
		t.Fatalf("partial order = %+v", quotes)
	}
	if quotes[1].Price != 2*poundsPerTonne {
		t.Fatalf("copper scale = %v", quotes[1].Price)
	}

	allDown := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	if got, err := fetchAll(context.Background(), allDown); err == nil || got != nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("all-down = %+v,%v", got, err)
	}
}

func TestCacheFreshAndRefreshedResultsDoNotAliasState(t *testing.T) {
	now := time.Now()
	cache := &Cache{
		quotes:  []Quote{{Symbol: "A", Label: "Alpha", Price: 10}},
		fetched: now,
		ttl:     time.Hour,
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("fresh cache unexpectedly fetched")
			return nil, nil
		})},
	}
	quotes, asOf, stale, err := cache.Summary(context.Background())
	if err != nil || stale || asOf != now.UnixMilli() || len(quotes) != 1 {
		t.Fatalf("fresh Summary = %+v,%d,%v,%v", quotes, asOf, stale, err)
	}
	quotes[0].Price = 999
	if cache.quotes[0].Price != 10 {
		t.Fatal("fresh result aliases cache")
	}

	responses := atomicResponses(chartBody(20, 19, "USD", 8))
	refresh := &Cache{ttl: time.Hour, client: &http.Client{Transport: responses}}
	fresh, _, stale, err := refresh.Summary(context.Background())
	if err != nil || stale || len(fresh) != len(catalog) {
		t.Fatalf("refresh Summary = %d,%v,%v", len(fresh), stale, err)
	}
	fresh[0].Price = 999
	if refresh.quotes[0].Price == 999 {
		t.Fatal("refreshed result aliases cache")
	}
}

func atomicResponses(body string) roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, body), nil
	}
}

func TestCacheFailureColdAndStaleFallbackIsolation(t *testing.T) {
	down := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	cold := &Cache{ttl: time.Minute, client: down}
	if quotes, asOf, stale, err := cold.Summary(context.Background()); err == nil || quotes != nil || asOf != 0 || stale {
		t.Fatalf("cold failure = %+v,%d,%v,%v", quotes, asOf, stale, err)
	}

	fetched := time.Now().Add(-time.Hour)
	cache := &Cache{
		quotes:  []Quote{{Symbol: "OLD", Price: 5}},
		fetched: fetched,
		ttl:     time.Minute,
		client:  down,
	}
	quotes, asOf, stale, err := cache.Summary(context.Background())
	if err != nil || !stale || asOf != fetched.UnixMilli() || len(quotes) != 1 {
		t.Fatalf("stale fallback = %+v,%d,%v,%v", quotes, asOf, stale, err)
	}
	quotes[0].Price = 999
	if cache.quotes[0].Price != 5 {
		t.Fatal("stale fallback aliases cache")
	}
}

func TestLetterTokensFilterMergeExpiryAndUnknownSyntax(t *testing.T) {
	resetLetterTokens()
	RecordLetterTokens(nil)
	RecordLetterTokens(map[string]string{"": "ignored", LetterTokenUSDKRW: "", LetterTokenEURKRW: "1,700"})
	RecordLetterTokens(map[string]string{LetterTokenUSDKRW: "1,500"})
	got := SubstituteLetterTokens(LetterTokenUSDKRW + "/" + LetterTokenEURKRW + "/{{market:unknown}}")
	if got != "1,500/1,700/—" {
		t.Fatalf("merged substitution = %q", got)
	}
	if got := SubstituteLetterTokens("{{market:UPPER}} {{market:with-dash}} {{other:x}}"); got != "{{market:UPPER}} {{market:with-dash}} {{other:x}}" {
		t.Fatalf("nonmatching token syntax changed: %q", got)
	}

	letterTokens.Lock()
	letterTokens.asOf = time.Now().Add(-letterTokenTTL - time.Second)
	letterTokens.Unlock()
	RecordLetterTokens(map[string]string{LetterTokenCopper: "9,000"})
	got = SubstituteLetterTokens(LetterTokenUSDKRW + "/" + LetterTokenCopper)
	if got != "—/9,000" {
		t.Fatalf("expired map was not reset: %q", got)
	}
}

func TestLetterTokensConcurrentRecordAndSubstitute(t *testing.T) {
	resetLetterTokens()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if i%2 == 0 {
					RecordLetterTokens(map[string]string{LetterTokenUSDKRW: intString(int64(i*1000 + j + 1))})
				} else {
					out := SubstituteLetterTokens("환율 " + LetterTokenUSDKRW)
					if strings.Contains(out, "{{market:") {
						t.Errorf("token leaked during concurrent substitution: %q", out)
						return
					}
				}
			}
		}(i)
	}
	wg.Wait()
}
