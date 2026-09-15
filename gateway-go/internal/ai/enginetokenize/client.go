// Package enginetokenize asks the local serving engine how many tokens a string
// really is, and caches the answer.
//
// This is a direct-connection capability: the engine exposes POST /tokenize,
// which renders through its own chat template and returns count, ids and
// max_model_len. An OpenAI-compatible proxy cannot carry that question, so the
// gateway could only ever estimate — and internal/ai/tokenest is a per-script
// heuristic that lands within about 12% of the truth, no better.
//
// Twelve percent of a 40K-token prompt head is 5K tokens, and the head sits
// inside a 45K budget. What the estimate decides is therefore not the head but
// the REMAINDER — the room left for tier-1 memory — where the same 5K is the
// whole quantity. Exactness there is the difference between admitting memory
// and dropping it for no reason.
//
// Safety: the same private-host rule as the other engine probes
// (pkg/httputil.IsPrivateHost). A public provider URL is never contacted, the
// request is bounded by a short timeout and a size cap, and every failure is a
// miss the caller answers with its estimate.
package enginetokenize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// Timeout bounds one call. Tokenizing is CPU work on the engine's rank 0.
	Timeout = 5 * time.Second

	// MaxTextBytes caps what a single call will send.
	MaxTextBytes = 1 << 20

	// cacheEntries bounds the count cache. The gateway has a handful of prompt
	// families (one per session kind), and the prompt-cache doctrine keeps each
	// one byte-stable across turns on purpose — which is exactly what makes a
	// content-hash cache almost always hit. A small cap is enough; a large one
	// would only retain heads nobody sends any more.
	cacheEntries = 32
)

// Client talks to one engine's /tokenize endpoint.
type Client struct {
	url  string
	http *http.Client
}

// New builds a client from any URL on the serving engine — its /metrics
// endpoint, its /v1 base, or the bare host. Returns nil when the URL is absent,
// malformed, carries credentials, or does not resolve to a host this deployment
// owns. A nil Client is safe to call and always misses.
func New(engineURL string) *Client {
	raw := strings.TrimSpace(engineURL)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	// Reject userinfo/query/fragment for the same reason the cache sampler
	// does: the endpoint string reaches logs.
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil
	}
	if !httputil.IsPrivateHost(u.Hostname()) {
		return nil
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/metrics")
	path = strings.TrimSuffix(path, "/v1")
	u.Path = path + "/tokenize"
	return &Client{url: u.String(), http: httputil.NewClient(Timeout)}
}

// Result is what the engine reports about a string.
type Result struct {
	// Count is the exact token count as this engine tokenizes it.
	Count int
	// MaxModelLen is the engine's served context ceiling, returned on every
	// call. It is the one place the gateway can learn the real window instead
	// of trusting a config value that drifts when the engine is reconfigured.
	MaxModelLen int
	// Normalized reports that the engine applied NFC and counted a different
	// string than the one sent. The count is still what generation would cost,
	// but it is not a measurement of the bytes given, so a caller comparing
	// against an estimator should discard it.
	Normalized bool
}

// Count asks the engine for the exact token count of text. ok=false on a nil
// client, an oversized input, or any transport, status or decode failure.
func (c *Client) Count(ctx context.Context, text string) (Result, bool) {
	if c == nil || text == "" || len(text) > MaxTextBytes {
		return Result{}, false
	}
	// add_special_tokens=false: the question is about THIS string, not about a
	// prompt the engine would frame with BOS. The endpoint defaults to true on
	// the prompt form, which would add a token the estimator never saw.
	body, err := json.Marshal(map[string]any{"prompt": text, "add_special_tokens": false})
	if err != nil {
		return Result{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, false
	}
	var out struct {
		Count       int    `json:"count"`
		MaxModelLen int    `json:"max_model_len"`
		Normalized  string `json:"normalized"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Count <= 0 {
		return Result{}, false
	}
	return Result{Count: out.Count, MaxModelLen: out.MaxModelLen, Normalized: out.Normalized != ""}, true
}

// Counter is a non-blocking exact-token oracle: a lookup answers from cache or
// says it does not know and fills in the background.
//
// A turn never waits on the engine. The first turn carrying a given prompt head
// uses its estimate, the fill lands, and every later turn on that head — which
// the prompt-cache doctrine keeps byte-identical — is exact.
type Counter struct {
	client *Client
	logger *slog.Logger

	mu       sync.Mutex
	cache    map[string]int  // content hash → tokens
	order    []string        // insertion order, for the bound
	inflight map[string]bool // fills already scheduled
}

// NewCounter returns a Counter for the engine at engineURL, or nil when that
// URL is unusable. A nil Counter always misses, which is the pre-existing
// behaviour: the caller estimates.
func NewCounter(engineURL string, logger *slog.Logger) *Counter {
	client := New(engineURL)
	if client == nil {
		return nil
	}
	return &Counter{
		client:   client,
		logger:   logger,
		cache:    make(map[string]int, cacheEntries),
		inflight: make(map[string]bool),
	}
}

// Exact returns the engine's token count for text when it is already known.
// On a miss it schedules one background fill and reports false so the caller
// falls back to its estimate. Never blocks.
func (c *Counter) Exact(text string) (int, bool) {
	if c == nil || text == "" || len(text) > MaxTextBytes {
		return 0, false
	}
	key := hashText(text)
	c.mu.Lock()
	if n, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return n, true
	}
	if c.inflight[key] {
		c.mu.Unlock()
		return 0, false
	}
	c.inflight[key] = true
	c.mu.Unlock()

	safego.GoWithSlog(c.logger, "engine-tokenize-fill", func() { c.fill(key, text) })
	return 0, false
}

func (c *Counter) fill(key, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	res, ok := c.client.Count(ctx, text)

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
	// A normalized answer counted a different string; caching it would report
	// the engine's NFC form as the size of the bytes we hold.
	if !ok || res.Normalized {
		return
	}
	if len(c.order) >= cacheEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.cache, oldest)
	}
	c.cache[key] = res.Count
	c.order = append(c.order, key)
	if c.logger != nil {
		c.logger.Debug("engine tokenize", "tokens", res.Count, "bytes", len(text), "maxModelLen", res.MaxModelLen)
	}
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}
