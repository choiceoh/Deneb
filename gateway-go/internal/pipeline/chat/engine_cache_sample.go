// engine_cache_sample.go samples a self-hosted vLLM engine's prefix-cache
// (APC) counters after a run and logs the delta as a run.cache agentlog
// event.
//
// Why: the local vLLM build does not report per-request cached_tokens in its
// usage payload, so run.end's CacheReadTokens stays 0 on the vLLM path and
// the gateway is blind to how well the APC prefix survives our prompt
// assembly. The engine's /metrics endpoint exposes cumulative token-level
// counters (vllm:prefix_cache_{hits,queries}_total); the delta between
// consecutive samples taken by this gateway attributes tokens to "whatever
// ran since the previous sample" — exact under the single-user, mostly-serial
// workload, smeared when runs overlap. This is the measurement that verifies
// the tail-injection work in run_tail_inject.go.
//
// Safety: strictly best-effort. Only loopback/private/CGNAT hosts are ever
// contacted (a public provider base URL never is), the scrape is bounded by
// a short timeout, and any failure just skips the event.
package chat

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	engineCacheSampleTimeout = 2 * time.Second
	metricPrefixCacheQueries = "vllm:prefix_cache_queries_total"
	metricPrefixCacheHits    = "vllm:prefix_cache_hits_total"
)

var engineCacheHTTP = httputil.NewClient(engineCacheSampleTimeout)

// engineCacheState remembers the last counter values per metrics URL so each
// sample can report a delta. In-memory only: the first sample after a gateway
// restart (or an engine restart, which resets the counters) just establishes
// a baseline and logs nothing.
var engineCacheState = struct {
	mu   sync.Mutex
	last map[string][2]float64 // metricsURL → {hits, queries}
}{last: make(map[string][2]float64)}

// logEngineCacheAsync samples the engine serving this run and emits a
// run.cache event. Fire-and-forget: never blocks the reply path. Skipped for
// non-OpenAI-mode providers, fallback runs (the sampled engine would not be
// the one that answered), and base URLs that don't resolve to a local engine.
func logEngineCacheAsync(deps runDeps, runLog *agentlog.RunLogger, client *llm.Client, apiMode string, fellBack bool, logger *slog.Logger) {
	if runLog == nil || client == nil || fellBack || apiMode != llm.APIModeOpenAI {
		return
	}
	metricsURL := engineMetricsURL(client.BaseURL())
	if metricsURL == "" {
		return
	}
	parentCtx := deps.callbacks.shutdownCtx
	if parentCtx == nil {
		parentCtx = context.Background() // tests; bounded by the timeout below
	}
	safego.GoWithSlog(logger, "engine-cache-sample", func() {
		ctx, cancel := context.WithTimeout(parentCtx, engineCacheSampleTimeout)
		defer cancel()
		hitDelta, queryDelta, ok := sampleEngineCacheDelta(ctx, metricsURL)
		if !ok {
			return
		}
		runLog.LogCache(agentlog.RunCacheData{
			EngineHitTokens:   hitDelta,
			EngineQueryTokens: queryDelta,
			MetricsURL:        metricsURL,
		})
	})
}

// engineMetricsURL derives the /metrics endpoint from an OpenAI-compatible
// base URL, but ONLY for hosts that are plausibly a self-hosted engine:
// loopback, RFC1918 private, or CGNAT 100.64/10 (Tailscale). Anything else —
// public providers, unresolvable hostnames — returns "" so the gateway never
// probes an external service.
func engineMetricsURL(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil {
			return ""
		}
		if !ip.IsLoopback() && !ip.IsPrivate() && !isCGNAT(ip) {
			return ""
		}
	}
	base := strings.TrimRight(u.Path, "/")
	base = strings.TrimSuffix(base, "/v1")
	u.Path = base + "/metrics"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// isCGNAT reports whether ip falls in 100.64.0.0/10 (carrier-grade NAT — the
// Tailscale address space, which is how this deployment reaches its second
// DGX node).
func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 0x40
}

// sampleEngineCacheDelta scrapes metricsURL and returns the hit/query token
// deltas since the previous sample of the same URL. ok=false on any fetch or
// parse failure, on the baseline (first) sample, and when a counter moved
// backwards (engine restart) — in the latter cases the new values are stored
// so the next sample is a clean delta.
func sampleEngineCacheDelta(ctx context.Context, metricsURL string) (hitDelta, queryDelta int64, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return 0, 0, false
	}
	resp, err := engineCacheHTTP.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false
	}

	var hits, queries float64
	var sawHits, sawQueries bool
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Counters may appear once per engine label set; sum them.
		if v, matched := metricValue(line, metricPrefixCacheHits); matched {
			hits += v
			sawHits = true
		} else if v, matched := metricValue(line, metricPrefixCacheQueries); matched {
			queries += v
			sawQueries = true
		}
	}
	if scanner.Err() != nil || !sawHits || !sawQueries {
		return 0, 0, false
	}

	engineCacheState.mu.Lock()
	defer engineCacheState.mu.Unlock()
	prev, hadPrev := engineCacheState.last[metricsURL]
	engineCacheState.last[metricsURL] = [2]float64{hits, queries}
	if !hadPrev || hits < prev[0] || queries < prev[1] {
		return 0, 0, false // baseline or engine restart
	}
	return int64(hits - prev[0]), int64(queries - prev[1]), true
}

// metricValue parses a Prometheus text-format sample line for the given
// metric name (with or without labels), returning its value.
func metricValue(line, name string) (float64, bool) {
	if !strings.HasPrefix(line, name) {
		return 0, false
	}
	rest := line[len(name):]
	// Must be followed by a label set or a space — not a longer metric name
	// sharing the prefix (e.g. ..._created).
	if !strings.HasPrefix(rest, "{") && !strings.HasPrefix(rest, " ") {
		return 0, false
	}
	idx := strings.LastIndexByte(line, ' ')
	if idx < 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(line[idx+1:]), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
