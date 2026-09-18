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
	"sync"
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

// engineState is one endpoint's hysteresis. Written only by the run
// goroutine, under Watcher.mu so State can read a consistent picture.
type engineState struct {
	healthURL string
	down      bool
	downSince time.Time
	upSince   time.Time
	reason    string
	silent    int
	ready     int
	probed    bool     // a verdict has been applied since this process started
	models    []string // last set handed to the sink while down
}

// TransitionEvent is one readiness change, handed to the OnTransition
// callback after the sink has been told. DownFor is set on an Up event.
type TransitionEvent struct {
	Endpoint string
	Down     bool
	At       time.Time
	Reason   string
	Models   []string
	DownFor  time.Duration
}

// EngineState is a point-in-time reading of one engine for a caller outside
// the probe loop (the engine RPC). Probed is false until the first verdict.
type EngineState struct {
	Endpoint  string
	Down      bool
	DownSince time.Time
	UpSince   time.Time
	Reason    string
	Models    []string
	Probed    bool
}

// Watcher probes each configured engine's /health and reports transitions.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	Watcher.mu  →  Ledger.mu   (ledger READS may run under mu)
//
// The sink (modelrole.Registry, its own lock), ledger writes and the
// transition callback run after mu is released — see observe.
type Watcher struct {
	endpoints []string
	engines   map[string]*engineState
	models    ModelsFunc
	sink      Sink
	client    *http.Client
	logger    *slog.Logger
	interval  time.Duration
	now       func() time.Time

	mu           sync.Mutex
	ledger       *Ledger
	onTransition func(TransitionEvent)
	startedAt    time.Time
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

// UseLedger persists transitions to l. Call before Start; nil-safe both ways.
func (w *Watcher) UseLedger(l *Ledger) {
	if w == nil {
		return
	}
	w.ledger = l
}

// OnTransition registers a callback for every readiness change, invoked on
// the probe goroutine after the sink has been updated. Call before Start.
func (w *Watcher) OnTransition(fn func(TransitionEvent)) {
	if w == nil {
		return
	}
	w.onTransition = fn
}

// State reads one engine's current standing. ok is false for an endpoint the
// watcher does not track (or a nil Watcher).
func (w *Watcher) State(endpoint string) (EngineState, bool) {
	if w == nil {
		return EngineState{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.engines[endpoint]
	if st == nil {
		return EngineState{}, false
	}
	return EngineState{
		Endpoint:  endpoint,
		Down:      st.down,
		DownSince: st.downSince,
		UpSince:   st.upSince,
		Reason:    st.reason,
		Models:    append([]string(nil), st.models...),
		Probed:    st.probed,
	}, true
}

// Transitions returns the ledger's transitions for endpoint since sinceMs
// (see Ledger.Transitions). Nil without a ledger.
func (w *Watcher) Transitions(endpoint string, sinceMs int64) []Transition {
	if w == nil {
		return nil
	}
	return w.ledger.Transitions(endpoint, sinceMs)
}

// TrackedSinceMs is the earliest moment anything is known about endpoint's
// availability: the ledger's first entry, or this watcher's start when the
// ledger has nothing older. ok is false before the watcher has started.
func (w *Watcher) TrackedSinceMs(endpoint string) (int64, bool) {
	if w == nil {
		return 0, false
	}
	w.mu.Lock()
	started := w.startedAt
	w.mu.Unlock()
	if started.IsZero() {
		return 0, false
	}
	since := started.UnixMilli()
	if first, ok := w.ledger.EarliestMs(endpoint); ok && first < since {
		since = first
	}
	return since, true
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
	w.mu.Lock()
	w.startedAt = w.now()
	w.mu.Unlock()
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
//
// State moves under w.mu; the sink, the ledger and the callback are told
// afterwards, outside it.
func (w *Watcher) observe(ep string, st *engineState, v verdict, reason string) {
	w.mu.Lock()
	first := !st.probed
	st.probed = true
	var effect func()
	switch v {
	case verdictReady:
		st.silent = 0
		switch {
		case st.down:
			st.ready++
			if st.ready >= readyToUp {
				effect = w.markUpLocked(ep, st)
			}
		case first:
			effect = w.foundReadyLocked(ep, st)
		}
	case verdictRefusing:
		st.silent, st.ready = 0, 0
		effect = w.markDownLocked(ep, st, reason)
	case verdictSilent:
		st.ready = 0
		st.silent++
		if st.down || st.silent >= silentToDown {
			effect = w.markDownLocked(ep, st, reason)
		}
	}
	w.mu.Unlock()
	if effect != nil {
		effect()
	}
}

// foundReadyLocked handles the first verdict of this process being "ready":
// nothing to change — unless the ledger's last word on this engine was a
// down. Then the outage ended while no gateway was watching, and it is closed
// now so it does not read as ongoing forever.
func (w *Watcher) foundReadyLocked(ep string, st *engineState) func() {
	now := w.now()
	st.upSince = now
	last, ok := w.ledger.Last(ep)
	if !ok || !last.Down {
		return nil
	}
	return func() {
		w.ledger.Record(Transition{
			Endpoint: ep, AtMs: now.UnixMilli(), Down: false,
			Reason: "found ready when the gateway started",
		})
	}
}

func (w *Watcher) markDownLocked(ep string, st *engineState, reason string) func() {
	models := normalizeModels(w.models(ep))
	var transition *Transition
	switch {
	case !st.down:
		now := w.now()
		st.down, st.downSince, st.reason = true, now, reason
		// A gateway restarted inside an outage finds the engine down again;
		// the outage began when the ledger says it did, not now.
		if last, ok := w.ledger.Last(ep); ok && last.Down {
			st.downSince = time.UnixMilli(last.AtMs)
		}
		transition = &Transition{Endpoint: ep, AtMs: now.UnixMilli(), Down: true, Reason: reason, Models: models}
		w.logger.Warn("local serving engine is refusing requests; its models go straight to fallback (advisory)",
			"endpoint", ep, "reason", reason, "models", strings.Join(models, ","))
	case !slices.Equal(models, st.models):
		w.logger.Info("engine liveness: models served by the down engine changed",
			"endpoint", ep, "models", strings.Join(models, ","))
	}
	st.models = models
	return func() {
		w.sink.SetEngineDown(ep, models)
		if transition != nil {
			w.ledger.Record(*transition)
			if w.onTransition != nil {
				w.onTransition(TransitionEvent{
					Endpoint: ep, Down: true, At: time.UnixMilli(transition.AtMs),
					Reason: reason, Models: models,
				})
			}
		}
	}
}

func (w *Watcher) markUpLocked(ep string, st *engineState) func() {
	now := w.now()
	downFor := now.Sub(st.downSince).Round(time.Second)
	models := st.models
	w.logger.Info("local serving engine is accepting requests again; its models rejoin routing",
		"endpoint", ep, "downFor", downFor.String(), "models", strings.Join(models, ","))
	st.down, st.ready, st.models, st.reason = false, 0, nil, ""
	st.upSince = now
	return func() {
		w.sink.SetEngineDown(ep, nil)
		w.ledger.Record(Transition{Endpoint: ep, AtMs: now.UnixMilli(), Down: false, Models: models})
		if w.onTransition != nil {
			w.onTransition(TransitionEvent{Endpoint: ep, Down: false, At: now, Models: models, DownFor: downFor})
		}
	}
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
