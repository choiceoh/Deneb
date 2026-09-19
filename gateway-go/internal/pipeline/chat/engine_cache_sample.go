// engine_cache_sample.go samples a self-hosted vLLM engine's prefix-cache
// (APC) counters after a run and logs the delta as a run.cache agentlog
// event.
//
// Why: the local vLLM build does not report per-request cached_tokens in its
// usage payload, so run.end's CacheReadTokens stays 0 on the vLLM path and
// the gateway is blind to how well the APC prefix survives our prompt
// assembly. The engine's /metrics endpoint exposes cumulative token-level
// counters (vllm:prefix_cache_{hits,queries}_total on vLLM,
// st:prefix_reused_tokens_total / vllm:prompt_tokens_total on ST); the delta between
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
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	engineCacheSampleTimeout = 2 * time.Second
	metricPrefixCacheQueries = "vllm:prefix_cache_queries_total"
	metricPrefixCacheHits    = "vllm:prefix_cache_hits_total"
	metricSTReusedTokens     = "st:prefix_reused_tokens_total" //nolint:gosec // G101: metric name
	metricPromptTokens       = "vllm:prompt_tokens_total"      //nolint:gosec // G101: metric name
)

var engineCacheHTTP = httputil.NewClient(engineCacheSampleTimeout)

// engineCacheState remembers the last counter values per metrics URL so each
// sample can report a delta. In-memory only: the first sample after a gateway
// restart (or an engine restart, which resets the counters) just establishes
// a baseline and logs nothing.
type engineCacheSample struct {
	hits, queries float64
	st            bool
}

var engineCacheState = struct {
	mu   sync.Mutex
	last map[string]engineCacheSample // metricsURL → token counters and source
}{last: make(map[string]engineCacheSample)}

// logEngineCacheAsync samples the engine that served this run and emits a
// run.cache event. Fire-and-forget: never blocks the reply path. Skipped for
// non-OpenAI-mode providers, fallback runs (the sampled engine would not be
// the one that answered), base URLs that don't resolve to a local engine, and
// runs no configured engine served (engineCacheSampleTarget).
func logEngineCacheAsync(deps runDeps, runLog *agentlog.RunLogger, client *llm.Client, model string, fellBack bool, logger *slog.Logger) {
	if deps.briefcaseMode {
		return
	}
	// Gate on the client's actual wire mode, not the config-derived apiMode:
	// providers without an explicit `api` field resolve to "" upstream while
	// the client still speaks OpenAI by default — an apiMode!=openai check
	// silently disabled sampling for exactly that common config.
	if runLog == nil || client == nil || fellBack || client.APIMode() != llm.APIModeOpenAI {
		return
	}
	route := engineRoute{baseURL: client.BaseURL(), model: model, engineModels: deps.engineModels}
	endpoints := resolveEngineMetricsURLs(route.baseURL)
	if len(endpoints) == 0 {
		return
	}
	parentCtx := deps.callbacks.shutdownCtx
	if parentCtx == nil {
		parentCtx = context.Background() // tests; bounded by the timeout below
	}
	safego.GoWithSlog(logger, "engine-cache-sample", func() {
		// Picked here rather than before the spawn: placing a run at an engine
		// reads the router's config file, which the reply path must not wait on.
		metricsURL := engineCacheSampleTarget(route, endpoints, logger)
		if metricsURL == "" {
			return
		}
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

// engineMetricsURLEnv pins the serving engines' /metrics endpoints explicitly,
// e.g. http://100.125.220.117:8000/metrics — comma-separated when the fleet
// serves more than one model locally. Needed since the wormhole cutover
// (2026-06-14): the provider baseURL points at the router (:18800), which
// serves no /metrics, so URL derivation fails and per-run APC sampling
// silently died — production agent-logs show zero run.cache events after the
// cutover.
//
// It is read as a list through enginespeed.Endpoints, the same parse the
// engine-speed task and the liveness watcher use. This package once read the
// raw value as one URL: a two-engine list parsed as the first host with the
// path "/metrics,http://…", so every scrape and every /tokenize call would
// have gone to a path that does not exist, and failed in silence.
const engineMetricsURLEnv = "DENEB_ENGINE_METRICS_URL"

// engineMetricsModelsEnv is the operator's explicit sampling scope: runs whose
// model name contains one of the comma-separated substrings (case-insensitive).
// When set it replaces the router-derived scope — kept so an existing drop-in
// keeps meaning what it meant — and is otherwise unneeded. A hand-kept list
// goes stale the moment the engine serves another model: the production value
// "glm-5.3-flash" left every Qwen run unsampled once the engine screen could
// switch the served model (2026-09-19).
const engineMetricsModelsEnv = "DENEB_ENGINE_METRICS_MODELS"

// engineScopeWarned holds the models already reported as served by an engine
// but excluded by engineMetricsModelsEnv — one warning per model per process.
var engineScopeWarned sync.Map

// engineUnplacedWarned holds the models already reported as admitted by
// engineMetricsModelsEnv while no engine among several is known to serve them —
// one warning per model per process.
var engineUnplacedWarned sync.Map

// engineCacheSampleTarget returns the engine whose delta belongs to this run,
// or "" when none does. One OpenAI-mode provider (wormhole) fronts both cloud
// and local models, so the configured engines say nothing about which of them
// answered: unscoped, a cloud run would log a local engine's unrelated APC
// delta under its own run ID — and with several engines, a local run would log
// the wrong engine's.
func engineCacheSampleTarget(route engineRoute, endpoints []string, logger *slog.Logger) string {
	served := route.servingEngine(endpoints)
	filter := strings.TrimSpace(os.Getenv(engineMetricsModelsEnv))
	if filter == "" {
		return served
	}
	if !engineMetricsModelAllowed(route.model, filter) {
		// The explicit list wins, but a list that drops a model an engine serves
		// is the silent blindness the router-derived scope exists to end: say so.
		if served != "" {
			if _, seen := engineScopeWarned.LoadOrStore(route.model, true); !seen {
				logger.Warn("engine cache sample: "+engineMetricsModelsEnv+" excludes a model the engine serves; its runs log no run.cache",
					"model", route.model, "filter", filter, "metricsURL", served)
			}
		}
		return ""
	}
	if served != "" {
		return served
	}
	// The list admits the run by its name alone. With one engine that settles
	// where to sample — what the list meant before it could name several.
	// Among several, a pick would log one engine's delta under a run it may
	// not have served, so the run goes unsampled, and that is said once.
	if len(endpoints) == 1 {
		return endpoints[0]
	}
	if _, seen := engineUnplacedWarned.LoadOrStore(route.model, true); !seen {
		logger.Warn("engine cache sample: "+engineMetricsModelsEnv+" admits a model no configured engine is known to serve; with several engines its runs log no run.cache",
			"model", route.model, "filter", filter, "engines", len(endpoints))
	}
	return ""
}

// engineRoute is what places a run at an engine: the address its client sends
// to, the model it asks for, and the router's entries at each engine
// (configresolve.EngineModels, handed in through HandlerConfig.EngineModels so
// this package keeps no runtime import). A nil engineModels leaves only runs
// sent straight to an engine placeable.
type engineRoute struct {
	baseURL      string
	model        string
	engineModels func(engineURL string) []string
}

// servingEngine returns the endpoint, among the configured engines, of the one
// that served the run — "" when none did. First the engine at the run's own
// address: a role pointed straight at an engine, or no configured list, so the
// endpoint was derived from the run's base URL. Else the engine the router
// sends the run's model to, under the exact name of an entry there. Exact
// names, never substrings: "glm-5.3" (cloud) must not pass for "glm-5.3-flash"
// (local), nor a vendor-prefixed "z-ai/glm-5.3-flash". And a request that went
// to a public host was no engine's whatever the model is called — the local
// engine and its cloud twin answer under the same name.
//
// The router's entries are re-read on every call, because the router
// hot-reloads its config. It lists each name once (the last entry winning),
// so at most one engine carries the model.
func (r engineRoute) servingEngine(endpoints []string) string {
	base := httputil.HostPort(r.baseURL)
	if base == "" {
		return ""
	}
	for _, endpoint := range endpoints {
		if httputil.HostPort(endpoint) == base {
			return endpoint
		}
	}
	if r.engineModels == nil || !isPrivateEngineHost(httputil.Hostname(r.baseURL)) {
		return ""
	}
	for _, endpoint := range endpoints {
		if slices.Contains(r.engineModels(endpoint), r.model) {
			return endpoint
		}
	}
	return ""
}

// engineMetricsModelAllowed applies the explicit substring scope. Pure for tests.
func engineMetricsModelAllowed(model, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	m := strings.ToLower(model)
	for _, part := range strings.Split(filter, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" && strings.Contains(m, part) {
			return true
		}
	}
	return false
}

// resolveEngineMetricsURLs lists the engines a run's cache sample may come
// from: the operator's list when set — each entry held to the private-host
// rule, so a typo cannot point the sampler at a public service — else the one
// endpoint derived from the run's own base URL.
func resolveEngineMetricsURLs(baseURL string) []string {
	listed := enginespeed.Endpoints()
	if len(listed) == 0 {
		if derived := engineMetricsURL(baseURL); derived != "" {
			return []string{derived}
		}
		return nil
	}
	var out []string
	for _, endpoint := range listed {
		u, err := url.Parse(endpoint)
		// Also reject userinfo/query/fragment: the endpoint is persisted
		// verbatim into run.cache events (MetricsURL), so embedded credentials
		// or tokens would leak into agent logs.
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			!isPrivateEngineHost(u.Hostname()) {
			continue // a misconfigured entry fails safe: never sampled, and no cue to derive
		}
		out = append(out, endpoint)
	}
	return out
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
	if !isPrivateEngineHost(u.Hostname()) {
		return ""
	}
	base := strings.TrimRight(u.Path, "/")
	base = strings.TrimSuffix(base, "/v1")
	u.Path = base + "/metrics"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// isPrivateEngineHost reports whether host is plausibly a self-hosted engine:
// localhost, loopback, RFC1918 private, or CGNAT 100.64/10 (Tailscale).
//
// The rule itself lives in pkg/httputil so the engine-speed sampler, which
// scrapes the same endpoints from another package, cannot drift into a second
// and differently-permissive copy of a security decision.
func isPrivateEngineHost(host string) bool {
	return httputil.IsPrivateHost(host)
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

	var hits, queries, reused, prompt float64
	var sawHits, sawQueries, sawReused, sawPrompt, isST bool
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "st:") || strings.Contains(line, `engine="st"`) {
			isST = true
		}
		// Counters may appear once per engine label set; sum them.
		if v, matched := metricValue(line, metricPrefixCacheHits); matched {
			hits += v
			sawHits = true
		} else if v, matched := metricValue(line, metricPrefixCacheQueries); matched {
			queries += v
			sawQueries = true
		} else if v, matched := metricValue(line, metricSTReusedTokens); matched {
			reused += v
			sawReused = true
		} else if v, matched := metricValue(line, metricPromptTokens); matched {
			prompt += v
			sawPrompt = true
		}
	}
	// ST exports prefix hits/queries as REQUESTS. Never log those as tokens.
	if isST {
		hits, queries = reused, prompt
		sawHits, sawQueries = sawReused, sawPrompt
	}
	if scanner.Err() != nil || !sawHits || !sawQueries {
		return 0, 0, false
	}

	engineCacheState.mu.Lock()
	defer engineCacheState.mu.Unlock()
	prev, hadPrev := engineCacheState.last[metricsURL]
	engineCacheState.last[metricsURL] = engineCacheSample{hits: hits, queries: queries, st: isST}
	if !hadPrev || prev.st != isST || hits < prev.hits || queries < prev.queries {
		return 0, 0, false // baseline or engine restart
	}
	return int64(hits - prev.hits), int64(queries - prev.queries), true
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
