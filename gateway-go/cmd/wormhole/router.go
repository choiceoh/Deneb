package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// maxBodyBytes caps an inbound request body. Long-context chats are large but
// bounded; this stops a runaway client from buffering us out of memory.
const maxBodyBytes = 32 << 20 // 32 MiB

// router fans /v1 requests out to upstream backends by model name.
// snapshot is the live, swappable view of the config: the parsed config plus its
// model lookup and the file mtime it was loaded from. The watcher re-reads the
// config file when its mtime advances and atomically swaps a fresh snapshot in,
// so a toggle written to the file (from the management RPC) takes effect within a
// few seconds — no restart. Handlers read via cur() (a lock-free atomic load).
type snapshot struct {
	cfg    config
	models map[string]modelEntry
	mtime  time.Time
}

func buildSnapshot(cfg config, mtime time.Time) *snapshot {
	m := make(map[string]modelEntry, len(cfg.Models))
	for _, e := range cfg.Models {
		m[e.Name] = e
	}
	return &snapshot{cfg: cfg, models: m, mtime: mtime}
}

// fleetRefreshInterval is how often the watcher re-polls SparkFleet for live
// models. Slower than the config mtime check (3s): discovery is an off-box HTTP
// call and model lifecycle changes on the order of minutes, not seconds.
const fleetRefreshInterval = 15 * time.Second

type router struct {
	path string // config path to watch ("" disables hot-reload)
	snap atomic.Pointer[snapshot]
	// fleet holds models discovered from SparkFleet (fleet.go), refreshed by the
	// watcher on fleetRefreshInterval. Separate from snap because it refreshes on
	// its own cadence (HTTP poll), independent of the config file's mtime. Never
	// nil after newRouter; lookup() consults it after configured models.
	fleet atomic.Pointer[map[string]modelEntry]
	// fleetState is the last-logged discovery state ("up:N" / "down"). Touched ONLY
	// by the watcher goroutine (the sole caller of refreshFleet), so it needs no
	// lock; it exists to log discovery on transitions instead of every 15s poll.
	fleetState string
	metrics    *metrics // per-request counters, exposed at GET /metrics
	client     *http.Client
	log        *slog.Logger
}

func newRouter(cfg config, path string, log *slog.Logger) *router {
	rt := &router{
		path: path,
		// Streaming client: NO overall timeout — SSE responses run long and the
		// request context cancels on client disconnect. Only the dial, TLS
		// handshake, and time-to-first-response-header are bounded.
		client: &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 120 * time.Second,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
		}},
		log:     log,
		metrics: newMetrics(),
	}
	rt.snap.Store(buildSnapshot(cfg, time.Time{}))
	empty := map[string]modelEntry{}
	rt.fleet.Store(&empty)
	return rt
}

// cur returns the live config snapshot (lock-free).
func (rt *router) cur() *snapshot { return rt.snap.Load() }

// lookup resolves a client-facing model name to its backend. Configured models
// win over SparkFleet-discovered ones: an explicit config entry is an operator
// override (e.g. to pin a key, protocol, or upstream id) and must beat discovery.
func (rt *router) lookup(name string) (modelEntry, bool) {
	if e, ok := rt.cur().models[name]; ok {
		return e, true
	}
	if f := rt.fleet.Load(); f != nil {
		if e, ok := (*f)[name]; ok {
			return e, true
		}
	}
	return modelEntry{}, false
}

// mergedModels returns the full routable set for display/listing — configured
// models (in config order) followed by discovered ones not shadowed by a config
// entry of the same name. Not used on the hot path (that's lookup); only by
// listModels.
func (rt *router) mergedModels() []modelEntry {
	s := rt.cur()
	out := make([]modelEntry, 0, len(s.cfg.Models))
	out = append(out, s.cfg.Models...)
	if f := rt.fleet.Load(); f != nil {
		for name, e := range *f {
			if _, shadowed := s.models[name]; !shadowed {
				out = append(out, e)
			}
		}
	}
	return out
}

// watch re-reads the config file when its mtime advances (so management toggles
// apply live) and re-polls SparkFleet for discovered models. It exits when ctx is
// cancelled.
func (rt *router) watch(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			rt.log.Error("config watcher panic", "panic", r)
		}
	}()
	// Discover once up front so fleet models are routable as soon as possible.
	rt.refreshFleet(ctx)
	if rt.path == "" && rt.cur().cfg.Sparkfleet == nil {
		return // nothing to poll: static config, no discovery
	}
	cfgTick := time.NewTicker(3 * time.Second)
	defer cfgTick.Stop()
	fleetTick := time.NewTicker(fleetRefreshInterval)
	defer fleetTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cfgTick.C:
			rt.reloadIfChanged()
		case <-fleetTick.C:
			rt.refreshFleet(ctx)
		}
	}
}

// refreshFleet re-polls SparkFleet and swaps in the freshly discovered model set.
// On a transient discovery error it KEEPS the last-known set — a single failed
// poll shouldn't drop every fleet route mid-flight (a stale entry just 502s and
// auto-fallback handles it). When the source is removed (hot-reload) it clears.
func (rt *router) refreshFleet(parent context.Context) {
	src := rt.cur().cfg.Sparkfleet
	if src == nil || src.URL == "" {
		rt.clearFleet()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	entries, err := discoverFleet(ctx, rt.client, *src)
	if err != nil {
		if rt.fleetState != "down" { // log the failure once, not every poll
			rt.log.Warn("sparkfleet discovery failing, keeping last known", "url", src.URL, "error", err)
			rt.fleetState = "down"
		}
		return
	}
	m := make(map[string]modelEntry, len(entries))
	for _, e := range entries {
		m[e.Name] = e
	}
	rt.fleet.Store(&m)
	if st := fmt.Sprintf("up:%d", len(m)); st != rt.fleetState { // log only on change
		rt.log.Info("sparkfleet discovery", "models", len(m))
		rt.fleetState = st
	}
}

// clearFleet drops all discovered models (the source was removed via hot-reload).
func (rt *router) clearFleet() {
	if f := rt.fleet.Load(); f == nil || len(*f) == 0 {
		return
	}
	empty := map[string]modelEntry{}
	rt.fleet.Store(&empty)
}

// reloadIfChanged re-reads the config file and swaps in a fresh snapshot when the
// file's mtime has advanced past the loaded one. Returns true if it reloaded. A
// parse error keeps the current snapshot (a half-written file never wedges us).
func (rt *router) reloadIfChanged() bool {
	st, err := os.Stat(rt.path)
	if err != nil || !st.ModTime().After(rt.cur().mtime) {
		return false
	}
	nc, err := loadConfig(rt.path)
	if err != nil {
		rt.log.Warn("config reload failed, keeping current", "error", err)
		return false
	}
	rt.snap.Store(buildSnapshot(nc, st.ModTime()))
	rt.log.Info("config reloaded", "models", len(nc.Models))
	logConfigWarnings(rt.log, nc) // surface a bad edit at reload, not on first request
	return true
}

func (rt *router) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", rt.chatCompletions)
	mux.HandleFunc("POST /v1/messages", rt.messages)
	mux.HandleFunc("GET /v1/models", rt.listModels)
	mux.HandleFunc("GET /status", rt.status)
	mux.HandleFunc("GET /metrics", rt.metricsHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// authed gates a request on the wormhole token. An empty configured token means
// "open" (dev/loopback) — main() warns loudly about that at boot.
func (rt *router) authed(w http.ResponseWriter, r *http.Request) bool {
	token := rt.cur().cfg.Token
	if token == "" {
		return true
	}
	if clientToken(r) != token {
		writeErr(w, http.StatusUnauthorized, "invalid wormhole token")
		return false
	}
	return true
}

// serve is the shared front-of-house for both protocol endpoints. It
// authenticates, reads the body, and routes by the requested model: an explicit
// model name goes straight to that backend (protocol-checked + egress-guarded),
// while the reserved "auto" name (when configured) hands off to serveAuto. proto
// is the endpoint's wire protocol and pathSuffix the upstream path. Both the
// OpenAI and Anthropic request bodies carry a top-level "model", so the read is
// protocol-agnostic.
func (rt *router) serve(w http.ResponseWriter, r *http.Request, proto, pathSuffix string) {
	// Observe every request from one place: wrap w to capture the final status —
	// an early 4xx, a forwarded upstream status, or an auto-routing 5xx — and
	// record it on return. This is the visibility wormhole lacked as the hot path.
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	w = sw
	model := ""
	defer func() {
		d := time.Since(start)
		rt.metrics.record(model, sw.status, d)
		rt.log.Debug("request", "model", model, "status", sw.status, "ms", d.Milliseconds())
	}()
	if !rt.authed(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read request body")
		return
	}
	model = extractModel(body)
	if model == "" {
		writeErr(w, http.StatusBadRequest, "missing 'model'")
		return
	}
	// "auto" (when configured) lets the client delegate the choice.
	if model == rt.autoName() && len(rt.cur().cfg.Auto) > 0 {
		rt.serveAuto(w, r, body, proto, pathSuffix)
		return
	}
	entry, ok := rt.lookup(model)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown model: "+model)
		return
	}
	// Local-first egress guard: a local-only request must not reach a cloud backend.
	if rt.localOnly(r) && !entry.isLocal() {
		writeErr(w, http.StatusForbidden, "model '"+model+"' is cloud-backed and blocked by local-only policy")
		return
	}
	if entry.protocol() != proto {
		writeErr(w, http.StatusBadRequest, wrongEndpointMsg(entry))
		return
	}
	out := body
	if entry.UpstreamModel != model {
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			out = rewritten
		}
	}
	if rt.cur().cfg.effortRoutingOn() {
		out = rt.applyThinking(entry, out)
	}
	rt.forward(w, r, entry, out, pathSuffix)
}

// chatCompletions serves OpenAI clients: POST /v1/chat/completions.
func (rt *router) chatCompletions(w http.ResponseWriter, r *http.Request) {
	rt.serve(w, r, protocolOpenAI, "/chat/completions")
}

// messages serves Anthropic clients: POST /v1/messages. No translation — the
// client already speaks Anthropic, so the request rides straight through.
func (rt *router) messages(w http.ResponseWriter, r *http.Request) {
	rt.serve(w, r, protocolAnthropic, "/messages")
}

// serveAuto delegates the model choice to wormhole: it tries the configured auto
// candidates — filtered to this endpoint's protocol and the egress guard — in
// order (local first), committing to the first that connects with a non-5xx
// status and falling through on an unreachable or 5xx backend. Fallback only
// happens before any bytes are streamed; once a candidate starts responding we
// ride it out.
func (rt *router) serveAuto(w http.ResponseWriter, r *http.Request, body []byte, proto, pathSuffix string) {
	cands := rt.autoCandidates(r, proto)
	if len(cands) == 0 {
		writeErr(w, http.StatusServiceUnavailable, "no eligible auto model for this protocol/policy")
		return
	}
	var lastErr error
	for _, entry := range cands {
		out := body
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			out = rewritten
		}
		if rt.cur().cfg.effortRoutingOn() {
			out = rt.applyThinking(entry, out)
		}
		resp, err := rt.doUpstream(r, entry, out, pathSuffix)
		if err != nil {
			rt.log.Warn("auto: candidate unreachable, trying next", "model", entry.Name, "error", err)
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 {
			rt.log.Warn("auto: candidate errored, trying next", "model", entry.Name, "status", resp.StatusCode)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("%s returned %d", entry.Name, resp.StatusCode)
			continue
		}
		rt.log.Info("auto routed", "model", entry.Name)
		streamResponse(w, resp)
		return
	}
	rt.log.Warn("auto: all candidates failed", "error", lastErr)
	writeErr(w, http.StatusBadGateway, "all auto candidates failed")
}

// autoCandidates returns the configured auto models eligible for this request, in
// order: those matching the endpoint's protocol and passing the egress guard.
func (rt *router) autoCandidates(r *http.Request, proto string) []modelEntry {
	auto := rt.cur().cfg.Auto
	out := make([]modelEntry, 0, len(auto))
	for _, name := range auto {
		e, ok := rt.lookup(name) // an auto candidate may be a discovered fleet model
		if !ok || e.protocol() != proto {
			continue
		}
		if rt.localOnly(r) && !e.isLocal() {
			continue
		}
		out = append(out, e)
	}
	return out
}

// autoName is the reserved model name that triggers auto-routing (default "auto").
func (rt *router) autoName() string {
	if n := rt.cur().cfg.AutoName; n != "" {
		return n
	}
	return "auto"
}

// wrongEndpointMsg points a client that hit the wrong protocol endpoint at the right one.
func wrongEndpointMsg(e modelEntry) string {
	if e.protocol() == protocolAnthropic {
		return "model '" + e.Name + "' speaks the anthropic protocol — use POST /v1/messages"
	}
	return "model '" + e.Name + "' speaks the openai protocol — use POST /v1/chat/completions"
}

// doUpstream builds and sends the upstream request, returning the response
// WITHOUT reading the body — so an auto-routing caller can inspect the status and
// fall back to the next candidate before committing to stream it. The upstream
// key is injected here (protocol-aware); the client never sees it.
func (rt *router) doUpstream(r *http.Request, entry modelEntry, body []byte, pathSuffix string) (*http.Response, error) {
	url := strings.TrimRight(entry.URL, "/") + pathSuffix
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	upReq.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(upReq, entry, r)
	return rt.client.Do(upReq)
}

// forward proxies a single model's request and streams the response back.
func (rt *router) forward(w http.ResponseWriter, r *http.Request, entry modelEntry, body []byte, pathSuffix string) {
	resp, err := rt.doUpstream(r, entry, body, pathSuffix)
	if err != nil {
		rt.log.Warn("upstream call failed", "model", entry.Name, "url", entry.URL, "error", err)
		writeErr(w, http.StatusBadGateway, "upstream unreachable: "+entry.Name)
		return
	}
	streamResponse(w, resp)
}

// streamResponse copies the upstream status, headers, and body straight back —
// flushing as chunks arrive so SSE tokens reach the client immediately.
func streamResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	// Copy upstream headers (Content-Type drives SSE vs JSON on the client side).
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // client gone
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// listModels returns the registry as an OpenAI /v1/models list so clients can
// discover what this wormhole serves.
func (rt *router) listModels(w http.ResponseWriter, r *http.Request) {
	if !rt.authed(w, r) {
		return
	}
	type modelRow struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	models := rt.mergedModels() // configured + SparkFleet-discovered
	rows := make([]modelRow, 0, len(models)+1)
	// Advertise the reserved auto name first so clients see they can delegate.
	if len(rt.cur().cfg.Auto) > 0 {
		rows = append(rows, modelRow{ID: rt.autoName(), Object: "model", OwnedBy: "wormhole-auto"})
	}
	for _, e := range models {
		// /v1/models is the OpenAI front's catalog — only models reachable via
		// POST /v1/chat/completions belong here. An anthropic-protocol model 400s
		// on that endpoint, so listing it would mislead a client (and a discovering
		// picker) into binding it to the OpenAI surface. Anthropic models are still
		// served on /v1/messages and enumerated in /status (with protocol).
		if e.protocol() != protocolOpenAI {
			continue
		}
		owner := "wormhole-cloud"
		if e.isLocal() {
			owner = "wormhole-local"
		}
		rows = append(rows, modelRow{ID: e.Name, Object: "model", OwnedBy: owner})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": rows})
}

// extractModel pulls just the "model" field out of an OpenAI request body without
// fully parsing it.
func extractModel(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &probe)
	return strings.TrimSpace(probe.Model)
}

// rewriteModel replaces the "model" field with upstream while preserving every
// other field's raw bytes (so no float/number reformatting or key reordering
// leaks into the forwarded request).
func rewriteModel(body []byte, upstream string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	enc, err := json.Marshal(upstream)
	if err != nil {
		return nil, err
	}
	fields["model"] = enc
	return json.Marshal(fields)
}

// clientToken pulls the wormhole token from a request, accepting both the OpenAI
// convention (Authorization: Bearer …) and the Anthropic one (x-api-key: …) so a
// client of either protocol authenticates the same way.
func clientToken(r *http.Request) string {
	if x := strings.TrimSpace(r.Header.Get("x-api-key")); x != "" {
		return x
	}
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

// writeErr emits an OpenAI-shaped error envelope so clients parse it the same way
// they would a real OpenAI error.
func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": msg, "type": "wormhole_error"},
	})
}
