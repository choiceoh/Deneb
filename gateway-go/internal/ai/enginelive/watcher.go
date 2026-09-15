// Package enginelive watches the local serving engines' readiness and tells a
// Sink which models each one cannot serve right now, so no caller spends
// retries on an engine that is refusing everything.
//
// Why a probe rather than the failures themselves: a failure is only learned
// after a caller has paid for it, and the LLM client's retry ladder makes that
// payment about 70 seconds per call. On 2026-09-14, with the engine down for
// three days, 205 chat turns replayed an already-failed retry chain before the
// fallback chain even started — over two minutes each. The engine states its
// own readiness at /health, including the handover drain in which the process
// is alive but refuses every new request, so the gateway can know before
// anyone asks.
//
// Safety: the same private-host rule as the other engine probes
// (pkg/httputil.IsPrivateHost). An endpoint on any other host is dropped at
// construction and never contacted.
package enginelive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Sink receives, per engine endpoint, the models to treat as unservable; no
// models means the engine is serving. modelrole.Registry implements it.
type Sink interface {
	SetEngineDown(endpoint string, models []string)
}

// ModelsFunc resolves which models an engine endpoint serves. It is asked on
// every probe while that engine is down, so a router config edited during an
// outage is picked up without a restart.
type ModelsFunc func(endpoint string) []string

const (
	probeInterval = 5 * time.Second
	probeTimeout  = 2 * time.Second

	// silentToDown: a probe that gets no answer at all (timeout, no route) must
	// repeat before it counts — one lost probe over the fleet's Wi-Fi is not an
	// outage. A refused connection or a 5xx from /health is the engine host's
	// own answer and counts at once.
	silentToDown = 2
	// readyToUp: an engine must answer ready twice before traffic returns, so a
	// process that flaps while booting does not pull turns onto itself between
	// two refusals.
	readyToUp = 2
)

// verdict is one probe's reading of an engine.
type verdict int

const (
	verdictReady    verdict = iota // answered, and not refusing
	verdictRefusing                // said it accepts nothing, or refused the connection
	verdictSilent                  // no answer
)

// engineState is one endpoint's hysteresis. Touched only by the run goroutine.
type engineState struct {
	healthURL string
	down      bool
	downSince time.Time
	silent    int
	ready     int
	models    []string // last set handed to the sink while down
}

// Watcher probes each configured engine's /health and reports transitions.
type Watcher struct {
	endpoints []string
	engines   map[string]*engineState
	models    ModelsFunc
	sink      Sink
	client    *http.Client
	logger    *slog.Logger
	interval  time.Duration
	now       func() time.Time
}

// New returns nil when there is nothing to watch — no endpoints, no sink, or
// no endpoint on a host this deployment owns — so the caller starts nothing.
// endpoints are the engine URLs as configured (DENEB_ENGINE_METRICS_URL holds
// their /metrics URLs); the probe goes to /health on the same server.
func New(endpoints []string, models ModelsFunc, sink Sink, logger *slog.Logger) *Watcher {
	if sink == nil || models == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &Watcher{
		engines:  make(map[string]*engineState),
		models:   models,
		sink:     sink,
		client:   httputil.NewClient(probeTimeout),
		logger:   logger,
		interval: probeInterval,
		now:      time.Now,
	}
	for _, ep := range endpoints {
		ep = strings.TrimSpace(ep)
		if ep == "" || w.engines[ep] != nil {
			continue
		}
		health, ok := healthURL(ep)
		if !ok {
			logger.Warn("engine liveness: endpoint is not on a private host; not probing it", "endpoint", ep)
			continue
		}
		w.endpoints = append(w.endpoints, ep)
		w.engines[ep] = &engineState{healthURL: health}
	}
	if len(w.endpoints) == 0 {
		return nil
	}
	return w
}

// healthURL turns a configured engine URL into its /health URL, refusing any
// host the deployment does not own.
func healthURL(endpoint string) (string, bool) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !httputil.IsPrivateHost(httputil.Hostname(endpoint)) {
		return "", false
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/health", "", "", ""
	return u.String(), true
}

// Start runs the probe loop until ctx ends. Safe on a nil Watcher.
func (w *Watcher) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.logger.Info("engine liveness watch started",
		"endpoints", strings.Join(w.endpoints, ","), "interval", w.interval.String())
	safego.GoWithSlog(w.logger, "engine-liveness-watch", func() {
		w.run(ctx)
	})
}

func (w *Watcher) run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		// Probe before the first tick: a gateway restarted in the middle of an
		// outage should not wait an interval to stop retrying.
		w.probeAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Watcher) probeAll(ctx context.Context) {
	for _, ep := range w.endpoints {
		if ctx.Err() != nil {
			return
		}
		st := w.engines[ep]
		v, reason := w.probe(ctx, st.healthURL)
		w.observe(ep, st, v, reason)
	}
}

// observe applies one verdict to an engine's hysteresis.
func (w *Watcher) observe(ep string, st *engineState, v verdict, reason string) {
	switch v {
	case verdictReady:
		st.silent = 0
		if !st.down {
			return
		}
		st.ready++
		if st.ready >= readyToUp {
			w.markUp(ep, st)
		}
	case verdictRefusing:
		st.silent, st.ready = 0, 0
		w.markDown(ep, st, reason)
	case verdictSilent:
		st.ready = 0
		st.silent++
		if st.down || st.silent >= silentToDown {
			w.markDown(ep, st, reason)
		}
	}
}

func (w *Watcher) markDown(ep string, st *engineState, reason string) {
	models := normalizeModels(w.models(ep))
	w.sink.SetEngineDown(ep, models)
	switch {
	case !st.down:
		st.down, st.downSince = true, w.now()
		w.logger.Warn("local serving engine is refusing requests; its models go straight to fallback",
			"endpoint", ep, "reason", reason, "models", strings.Join(models, ","))
	case !slices.Equal(models, st.models):
		w.logger.Info("engine liveness: models served by the down engine changed",
			"endpoint", ep, "models", strings.Join(models, ","))
	}
	st.models = models
}

func (w *Watcher) markUp(ep string, st *engineState) {
	w.sink.SetEngineDown(ep, nil)
	w.logger.Info("local serving engine is accepting requests again; its models rejoin routing",
		"endpoint", ep, "downFor", w.now().Sub(st.downSince).Round(time.Second).String(),
		"models", strings.Join(st.models, ","))
	st.down, st.ready, st.models = false, 0, nil
}

// probe reads one engine's readiness. Three answers:
//   - a response below 500: ready. An engine without /health still answered,
//     so it is alive; only an explicit refusal takes traffic away.
//   - a 5xx, or a refused connection: refusing. /health answers 503 while the
//     engine is stopping or draining for a handover, and a refused connection
//     means nothing listens on the port.
//   - no answer: silent.
func (w *Watcher) probe(ctx context.Context, health string) (verdict, string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
	if err != nil {
		return verdictSilent, err.Error()
	}
	resp, err := w.client.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return verdictRefusing, "connection refused"
		}
		return verdictSilent, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < http.StatusInternalServerError {
		return verdictReady, ""
	}
	return verdictRefusing, refusalReason(resp.StatusCode, body)
}

// refusalReason names what the engine said, when it said something: the ST
// engine's /health carries {"status":"draining","handing_over_to":...}.
func refusalReason(status int, body []byte) string {
	reason := "health " + strconv.Itoa(status)
	var payload struct {
		Status        string `json:"status"`
		HandingOverTo string `json:"handing_over_to"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Status == "" {
		return reason
	}
	reason += " " + payload.Status
	if payload.HandingOverTo != "" {
		reason += " (handing over to " + payload.HandingOverTo + ")"
	}
	return reason
}

// normalizeModels trims, de-duplicates and sorts, so an unchanged set compares
// equal across probes.
func normalizeModels(models []string) []string {
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}
