package observe

import (
	"bufio"
	"context"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// VllmPrefixCache holds one served model's prefix-cache counters scraped from
// a vLLM /metrics endpoint. Counters are cumulative since engine boot — they
// are NOT scoped to any behavior window the caller may be reporting on.
type VllmPrefixCache struct {
	Model      string  `json:"model"`
	Queries    int64   `json:"queries"`
	Hits       int64   `json:"hits"`
	HitRatePct float64 `json:"hitRatePct"` // hits/queries*100, one decimal
}

// vllmMetricsTimeout bounds the /metrics scrape. The endpoint is local (or
// tailnet-local) and answers in milliseconds when healthy; a down server
// should fail fast instead of stalling an observe call.
const vllmMetricsTimeout = 2 * time.Second

// vllmPrefixCacheMetrics are the only two series we parse — engine-level
// prefix-cache traffic, labeled per served model.
const (
	vllmPrefixQueriesMetric = "vllm:prefix_cache_queries_total"
	vllmPrefixHitsMetric    = "vllm:prefix_cache_hits_total"
)

// FetchVllmPrefixCaches scrapes each vLLM base URL ("http://host:port/v1")
// for the prefix-cache counters, stripping the /v1 suffix to reach the
// engine's Prometheus /metrics endpoint. Endpoints that are down, non-vLLM
// (no such route / no vllm: series), or otherwise unreadable contribute
// nothing — callers render whatever comes back and stay silent otherwise.
func FetchVllmPrefixCaches(ctx context.Context, baseURLs []string) []VllmPrefixCache {
	if len(baseURLs) == 0 {
		return nil
	}
	client := httputil.NewClient(vllmMetricsTimeout)
	var out []VllmPrefixCache
	for _, base := range baseURLs {
		out = append(out, scrapeVllmPrefixCache(ctx, client, base)...)
	}
	return out
}

func scrapeVllmPrefixCache(ctx context.Context, client *http.Client, baseURL string) []VllmPrefixCache {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	metricsURL := strings.TrimSuffix(base, "/v1") + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil // server down → silently skip (graceful degradation)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil // non-vLLM endpoint (404 etc.) → silently skip
	}

	// Sum per model_name label; multiple engine shards ("engine" label) of the
	// same model collapse into one row. A busy engine's /metrics runs to a few
	// hundred KB — cap the read so a pathological endpoint can't balloon us.
	queries := make(map[string]float64)
	hits := make(map[string]float64)
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		if model, v, ok := parseVllmCounter(line, vllmPrefixQueriesMetric); ok {
			queries[model] += v
		} else if model, v, ok := parseVllmCounter(line, vllmPrefixHitsMetric); ok {
			hits[model] += v
		}
	}
	// A scan error mid-body just means a partial sum — still useful, keep it.

	models := make(map[string]bool, len(queries))
	for m := range queries {
		models[m] = true
	}
	for m := range hits {
		models[m] = true
	}
	names := make([]string, 0, len(models))
	for m := range models {
		names = append(names, m)
	}
	sort.Strings(names)

	out := make([]VllmPrefixCache, 0, len(names))
	for _, m := range names {
		q, h := queries[m], hits[m]
		pct := 0.0
		if q > 0 {
			pct = math.Round(h/q*1000) / 10
		}
		out = append(out, VllmPrefixCache{Model: m, Queries: int64(q), Hits: int64(h), HitRatePct: pct})
	}
	return out
}

// parseVllmCounter matches one Prometheus text-format sample line against an
// exact metric name and returns its model_name label and value. Comments,
// other metrics (including longer names sharing the prefix, e.g. *_created),
// and malformed samples return ok=false. An unlabeled sample (single-model
// server) returns model="".
func parseVllmCounter(line, name string) (model string, value float64, ok bool) {
	rest, found := strings.CutPrefix(line, name)
	if !found || rest == "" {
		return "", 0, false
	}
	switch rest[0] {
	case '{':
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", 0, false
		}
		model = promLabel(rest[1:end], "model_name")
		rest = rest[end+1:]
	case ' ', '\t':
		// no label set
	default:
		return "", 0, false // longer metric name sharing this prefix
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", 0, false
	}
	return model, v, true
}

// promLabel extracts one label's value from a Prometheus label body
// `a="x",b="y"`. Served model names never contain escaped quotes, so a plain
// quote scan is enough here. Key matches are anchored at a label boundary so
// e.g. a hypothetical "engine_model_name" label cannot shadow "model_name".
func promLabel(body, key string) string {
	needle := key + `="`
	for idx := strings.Index(body, needle); idx >= 0; {
		if idx == 0 || body[idx-1] == ',' || body[idx-1] == ' ' {
			rest := body[idx+len(needle):]
			if end := strings.IndexByte(rest, '"'); end >= 0 {
				return rest[:end]
			}
			return ""
		}
		next := strings.Index(body[idx+1:], needle)
		if next < 0 {
			return ""
		}
		idx += 1 + next
	}
	return ""
}
