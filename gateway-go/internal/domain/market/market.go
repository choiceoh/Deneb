// Package market fetches a small, fixed set of market quotes (FX, an index, and
// two commodities) for the Andromeda 오늘 dashboard's 시장 card. It uses Yahoo
// Finance's keyless chart endpoint and caches the result so the dashboard glance
// never hammers the upstream — one refresh per TTL, shared across callers.
//
// Local-first note: this is the rare outbound call the project otherwise avoids.
// It is opt-in (the 시장 card is hidden by default), bounded (4 symbols), and
// cached (10m), and it degrades gracefully — a fetch failure serves the last good
// snapshot (marked stale) rather than erroring the dashboard.
package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Quote is one instrument's latest price and prior close (for a change figure).
type Quote struct {
	Symbol    string  // Yahoo symbol, e.g. "^KS11"
	Label     string  // Korean display label, e.g. "코스피"
	Currency  string  // quote currency reported by Yahoo (KRW/USD/…)
	Price     float64 // latest price
	PrevClose float64 // previous close — change = Price - PrevClose
	AsOf      int64   // quote time, epoch millis (0 when unknown)
}

// poundsPerTonne converts COMEX copper (HG=F, quoted USD/lb) into USD per metric
// tonne — the standard basis people read copper in.
const poundsPerTonne = 2204.6226

// catalog is the fixed instrument set the 시장 card shows. Order is the display
// order. Kept server-side (not client-configurable) — opinionated defaults. scale
// multiplies the raw Yahoo price into the display unit (1 = as quoted).
var catalog = []struct {
	symbol, label string
	scale         float64
}{
	{"KRW=X", "원/달러", 1},
	{"^KS11", "코스피", 1},
	{"CL=F", "WTI 유가", 1},
	{"HG=F", "구리", poundsPerTonne}, // USD/lb → USD/tonne
}

const (
	yahooBase  = "https://query1.finance.yahoo.com/v8/finance/chart/"
	defaultTTL = 10 * time.Minute
	httpBudget = 8 * time.Second
)

// Cache holds the last fetched snapshot and refreshes it lazily once the TTL
// lapses. Safe for concurrent use; the outbound fetch runs without the lock held.
type Cache struct {
	mu      sync.Mutex
	quotes  []Quote
	fetched time.Time
	ttl     time.Duration
	client  *http.Client
}

// NewCache builds a market cache with the default TTL and a bounded HTTP client.
func NewCache() *Cache {
	return &Cache{ttl: defaultTTL, client: httputil.NewClient(httpBudget)}
}

// Summary returns the current quotes, the snapshot time (epoch millis), and a
// stale flag. A fresh cache hit returns immediately; otherwise it refreshes from
// Yahoo. On a refresh failure with a prior snapshot, the last good data is served
// with stale=true; with no prior snapshot, the error is returned.
func (c *Cache) Summary(ctx context.Context) (quotes []Quote, asOf int64, stale bool, err error) {
	c.mu.Lock()
	if len(c.quotes) > 0 && time.Since(c.fetched) < c.ttl {
		q, f := c.quotes, c.fetched
		c.mu.Unlock()
		return q, f.UnixMilli(), false, nil
	}
	c.mu.Unlock()

	// Fetch outside the lock — concurrency.md forbids holding a mutex across an
	// outbound call. Single-user serial traffic makes a double-fetch race a
	// non-issue (worst case: two refreshes), and the cache below is last-writer.
	fresh, ferr := fetchAll(ctx, c.client)

	c.mu.Lock()
	defer c.mu.Unlock()
	if ferr != nil {
		if len(c.quotes) > 0 {
			return c.quotes, c.fetched.UnixMilli(), true, nil // serve stale on failure
		}
		return nil, 0, false, ferr
	}
	c.quotes = fresh
	c.fetched = time.Now()
	return c.quotes, c.fetched.UnixMilli(), false, nil
}

// fetchAll pulls every catalog symbol. A per-symbol failure is skipped (partial
// result is fine for a glance); it errors only when nothing came back.
func fetchAll(ctx context.Context, client *http.Client) ([]Quote, error) {
	out := make([]Quote, 0, len(catalog))
	var lastErr error
	for _, item := range catalog {
		q, err := fetchOne(ctx, client, item.symbol, item.label, item.scale)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("market: no quotes")
	}
	return out, nil
}

func fetchOne(ctx context.Context, client *http.Client, symbol, label string, scale float64) (Quote, error) {
	u := yahooBase + url.PathEscape(symbol) + "?interval=1d&range=1d"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Quote{}, err
	}
	// Yahoo rejects empty/scripted User-Agents; a browser-like UA is required.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Deneb)")
	resp, err := client.Do(req)
	if err != nil {
		return Quote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Quote{}, fmt.Errorf("yahoo %s: HTTP %d", symbol, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Quote{}, err
	}
	return parseChart(body, symbol, label, scale)
}

// parseChart extracts the latest price + previous close from a Yahoo chart
// response, applying scale to convert into the display unit. Split out from the HTTP
// path so it is unit-testable without network.
func parseChart(body []byte, symbol, label string, scale float64) (Quote, error) {
	var parsed struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					ChartPreviousClose float64 `json:"chartPreviousClose"`
					PreviousClose      float64 `json:"previousClose"`
					Currency           string  `json:"currency"`
					RegularMarketTime  int64   `json:"regularMarketTime"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Quote{}, err
	}
	if len(parsed.Chart.Result) == 0 {
		return Quote{}, fmt.Errorf("yahoo %s: empty result", symbol)
	}
	m := parsed.Chart.Result[0].Meta
	if m.RegularMarketPrice == 0 {
		return Quote{}, fmt.Errorf("yahoo %s: no price", symbol)
	}
	prev := m.ChartPreviousClose
	if prev == 0 {
		prev = m.PreviousClose
	}
	if scale <= 0 {
		scale = 1
	}
	return Quote{
		Symbol:    symbol,
		Label:     label,
		Currency:  m.Currency,
		Price:     m.RegularMarketPrice * scale,
		PrevClose: prev * scale,
		AsOf:      m.RegularMarketTime * 1000,
	}, nil
}
