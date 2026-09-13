package observe

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// The serving engine's own latency, token and occupancy series, read from the
// Prometheus text it exposes at /metrics. It publishes the vLLM series names so
// a scraper written for the engine it replaces keeps working (stkernel
// engine/base/serve.py).
//
// Reading the engine is what makes per-model speed measurable WITHOUT
// instrumenting the gateway: the engine times its own work, so the numbers
// carry no network hop and no response-header buffering — the two things that
// made a gateway-side clock report a 0ms prefill on this fleet.
const (
	// Histograms: _sum is total seconds, _count is observations.
	engineTTFTSumMetric   = "vllm:time_to_first_token_seconds_sum"
	engineTTFTCountMetric = "vllm:time_to_first_token_seconds_count"
	// TTFT is "arrival to first token, QUEUEING INCLUDED", so the queue series
	// comes off it to leave the prefill itself.
	engineQueueSumMetric = "vllm:request_queue_time_seconds_sum"
	// One observation per decode step, valued at that step's seconds divided by
	// the tokens it produced — so count/sum is tokens per second.
	engineTPOTSumMetric   = "vllm:time_per_output_token_seconds_sum"
	engineTPOTCountMetric = "vllm:time_per_output_token_seconds_count"
	// "admission to the answer": the residency of a request, summed.
	engineE2ESumMetric   = "vllm:e2e_request_latency_seconds_sum"
	engineE2ECountMetric = "vllm:e2e_request_latency_seconds_count"
	// "one model step, host-observed end to end", labeled by kind
	// (prefill/decode). The engine only steps when it has work, so this sums to
	// the time it was BUSY.
	engineStepSumMetric = "st:step_seconds_sum"

	// gosec reads "Tokens" in a name assigned a string literal as a credential.
	// These are Prometheus series names for counts of language-model tokens.
	enginePromptTokensMetric = "vllm:prompt_tokens_total" //nolint:gosec // G101: a metric name, not a secret
	engineGenTokensMetric    = "vllm:generation_tokens_total"

	// Gauges — the occupancy right now, not a running total.
	engineRunningMetric = "vllm:num_requests_running"
	engineWaitingMetric = "vllm:num_requests_waiting"
)

// engineCumulativeMetrics are the series that only ever grow (until the engine
// restarts), and so may be differenced across a window.
var engineCumulativeMetrics = []string{
	engineTTFTSumMetric, engineTTFTCountMetric, engineQueueSumMetric,
	engineTPOTSumMetric, engineTPOTCountMetric,
	engineE2ESumMetric, engineE2ECountMetric, engineStepSumMetric,
	enginePromptTokensMetric, engineGenTokensMetric,
}

// engineGaugeMetrics are point-in-time and must never be differenced.
var engineGaugeMetrics = []string{engineRunningMetric, engineWaitingMetric}

// engineScrapeTimeout bounds one scrape. The endpoint is on the fleet's tailnet
// and answers in milliseconds when healthy; a booting or dead engine must fail
// fast rather than stall the sampler.
const engineScrapeTimeout = 3 * time.Second

// EngineCounters is one scrape of one serving engine.
//
// There is no per-model label to key on: this engine tags its series
// engine="st" and serves a single model, so the model identity belongs to the
// ENDPOINT and is read from its /v1/models. A second local model means a second
// endpoint.
type EngineCounters struct {
	// Model is what the engine says it serves; "" when /v1/models was
	// unreadable (the counters still stand, just unlabeled).
	Model string `json:"model,omitempty"`

	// Cumulative since the engine last started. Differencing two scrapes of
	// these is how a window is measured.
	TTFTSeconds      float64 `json:"ttftSeconds"`
	TTFTCount        float64 `json:"ttftCount"`
	QueueSeconds     float64 `json:"queueSeconds"`
	TPOTSeconds      float64 `json:"tpotSeconds"`
	TPOTCount        float64 `json:"tpotCount"`
	E2ESeconds       float64 `json:"e2eSeconds"`
	E2ECount         float64 `json:"e2eCount"`
	BusySeconds      float64 `json:"busySeconds"`
	PromptTokens     float64 `json:"promptTokens"`
	GenerationTokens float64 `json:"generationTokens"`

	// Occupancy AT THE MOMENT OF THE SCRAPE. Never differenced — a peak is
	// found by watching these, not by subtracting them.
	RunningRequests float64 `json:"runningRequests"`
	WaitingRequests float64 `json:"waitingRequests"`
}

// Concurrency is how many requests the engine held at the instant of this
// scrape: the ones it is stepping plus the ones admitted behind them.
func (c EngineCounters) Concurrency() int {
	n := int(c.RunningRequests + c.WaitingRequests)
	if n < 0 {
		return 0
	}
	return n
}

// FetchEngineCounters scrapes one engine's /metrics and names the model it
// serves. The private-host rule is applied here too, so a scraper reached
// through any call path cannot be aimed at a public service.
//
// ok=false when the endpoint is unreachable, not an engine, or exposes none of
// the series — an absent measurement, never a zeroed one.
func FetchEngineCounters(ctx context.Context, metricsURL string) (EngineCounters, bool) {
	var out EngineCounters
	if !httputil.IsPrivateHost(httputil.Hostname(metricsURL)) {
		return out, false
	}
	client := httputil.NewClient(engineScrapeTimeout)

	body, ok := getBody(ctx, client, metricsURL)
	if !ok {
		return out, false
	}
	defer body.Close()

	// One pass, summing each series over whatever label sets the engine splits
	// it into (st:step_seconds is per kind; shards would add more).
	totals := map[string]float64{}
	sc := bufio.NewScanner(io.LimitReader(body, 4<<20))
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		for _, name := range engineCumulativeMetrics {
			if _, v, matched := parseVllmCounter(line, name); matched {
				totals[name] += v
				break
			}
		}
		for _, name := range engineGaugeMetrics {
			if _, v, matched := parseVllmCounter(line, name); matched {
				totals[name] += v
				break
			}
		}
	}
	if len(totals) == 0 {
		return EngineCounters{}, false // reachable, but not an engine we can read
	}

	out = EngineCounters{
		TTFTSeconds:      totals[engineTTFTSumMetric],
		TTFTCount:        totals[engineTTFTCountMetric],
		QueueSeconds:     totals[engineQueueSumMetric],
		TPOTSeconds:      totals[engineTPOTSumMetric],
		TPOTCount:        totals[engineTPOTCountMetric],
		E2ESeconds:       totals[engineE2ESumMetric],
		E2ECount:         totals[engineE2ECountMetric],
		BusySeconds:      totals[engineStepSumMetric],
		PromptTokens:     totals[enginePromptTokensMetric],
		GenerationTokens: totals[engineGenTokensMetric],
		RunningRequests:  totals[engineRunningMetric],
		WaitingRequests:  totals[engineWaitingMetric],
	}
	out.Model = fetchServedModel(ctx, client, metricsURL)
	return out, true
}

// EngineDelta is the growth of the cumulative series across one interval.
// Deltas add, so a day is the sum of its intervals — which is what lets a
// restart cost only the interval it falls in.
type EngineDelta struct {
	TTFTSeconds      float64
	QueueSeconds     float64
	TPOTSeconds      float64
	TPOTCount        float64
	E2ESeconds       float64
	BusySeconds      float64
	Requests         float64
	PromptTokens     float64
	GenerationTokens float64
}

// EngineDeltaBetween is the growth from one scrape to the next.
//
// ok=false when any counter moved BACKWARDS, which means the engine restarted
// between the scrapes: the counters reset to zero, so the difference is not a
// delta and any rate from it is fiction. The caller re-baselines on `to` — a
// restart costs the interval it spans, not the series.
func EngineDeltaBetween(from, to EngineCounters) (EngineDelta, bool) {
	if to.TTFTSeconds < from.TTFTSeconds || to.TTFTCount < from.TTFTCount ||
		to.QueueSeconds < from.QueueSeconds ||
		to.TPOTSeconds < from.TPOTSeconds || to.TPOTCount < from.TPOTCount ||
		to.E2ESeconds < from.E2ESeconds || to.E2ECount < from.E2ECount ||
		to.BusySeconds < from.BusySeconds ||
		to.PromptTokens < from.PromptTokens || to.GenerationTokens < from.GenerationTokens {
		return EngineDelta{}, false
	}
	return EngineDelta{
		TTFTSeconds:      to.TTFTSeconds - from.TTFTSeconds,
		QueueSeconds:     to.QueueSeconds - from.QueueSeconds,
		TPOTSeconds:      to.TPOTSeconds - from.TPOTSeconds,
		TPOTCount:        to.TPOTCount - from.TPOTCount,
		E2ESeconds:       to.E2ESeconds - from.E2ESeconds,
		BusySeconds:      to.BusySeconds - from.BusySeconds,
		Requests:         to.TTFTCount - from.TTFTCount,
		PromptTokens:     to.PromptTokens - from.PromptTokens,
		GenerationTokens: to.GenerationTokens - from.GenerationTokens,
	}, true
}

// Add folds another interval into this one.
func (d EngineDelta) Add(o EngineDelta) EngineDelta {
	d.TTFTSeconds += o.TTFTSeconds
	d.QueueSeconds += o.QueueSeconds
	d.TPOTSeconds += o.TPOTSeconds
	d.TPOTCount += o.TPOTCount
	d.E2ESeconds += o.E2ESeconds
	d.BusySeconds += o.BusySeconds
	d.Requests += o.Requests
	d.PromptTokens += o.PromptTokens
	d.GenerationTokens += o.GenerationTokens
	return d
}

// EngineRates is what a window of engine work amounts to. Every rate is
// token- or time-weighted over exactly the requests the engine served in it.
type EngineRates struct {
	// PrefillTokensPerSec is prompt tokens over the seconds the engine spent
	// reaching their first output token, with its own queue wait removed.
	PrefillTokensPerSec float64 `json:"prefillTokensPerSec,omitempty"`
	// DecodeTokensPerSec is the reciprocal of the mean per-output-token time.
	DecodeTokensPerSec float64 `json:"decodeTokensPerSec,omitempty"`
	// ConcurrencyWhileBusy is the mean number of requests resident while the
	// engine was stepping — request-seconds over busy seconds. Idle time is
	// not in the denominator, so this is the average WHILE IN USE and never
	// dilutes toward zero on a quiet day.
	ConcurrencyWhileBusy float64 `json:"concurrencyWhileBusy,omitempty"`

	// Sample mass, so a reader can tell a settled number from one request.
	Requests        int64 `json:"requests"`
	PromptTokens    int64 `json:"promptTokens"`
	GeneratedTokens int64 `json:"generatedTokens"`
}

// Measured reports whether any rate rests on real work.
func (r EngineRates) Measured() bool {
	return r.PrefillTokensPerSec > 0 || r.DecodeTokensPerSec > 0 || r.ConcurrencyWhileBusy > 0
}

// Rates turns an accumulated delta into the window's rates. Each is computed
// only where its own denominator is real, so a window with decode but no
// prefill reports the half it measured rather than a zero for both.
func (d EngineDelta) Rates() EngineRates {
	out := EngineRates{
		Requests:        int64(d.Requests),
		PromptTokens:    int64(d.PromptTokens),
		GeneratedTokens: int64(d.GenerationTokens),
	}
	if prefillSeconds := d.TTFTSeconds - d.QueueSeconds; prefillSeconds > 0 && d.PromptTokens > 0 {
		out.PrefillTokensPerSec = d.PromptTokens / prefillSeconds
	}
	if d.TPOTSeconds > 0 {
		out.DecodeTokensPerSec = d.TPOTCount / d.TPOTSeconds
	}
	if d.BusySeconds > 0 && d.E2ESeconds > 0 {
		out.ConcurrencyWhileBusy = d.E2ESeconds / d.BusySeconds
	}
	return out
}

// fetchServedModel asks the same engine which model it is serving. Best effort:
// the counters stand on their own, and an unnamed engine is still measurable.
func fetchServedModel(ctx context.Context, client *http.Client, metricsURL string) string {
	modelsURL := strings.TrimSuffix(strings.TrimRight(metricsURL, "/"), "/metrics") + "/v1/models"
	body, ok := getBody(ctx, client, modelsURL)
	if !ok {
		return ""
	}
	defer body.Close()

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&payload) != nil {
		return ""
	}
	if len(payload.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.Data[0].ID)
}

func getBody(ctx context.Context, client *http.Client, rawURL string) (io.ReadCloser, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, false
	}
	return resp.Body, true
}
