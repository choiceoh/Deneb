package main

import (
	"bytes"
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

// maxUpstreamRetries bounds retries of a transient upstream failure (connection
// error or 5xx) on the explicit-model path before the error reaches the client.
// Retried only BEFORE any bytes stream (doUpstream returns before reading the
// body), so a completion is never half-sent twice. retryBackoffBase × attempt is
// the inter-retry delay.
const (
	maxUpstreamRetries = 2
	retryBackoffBase   = 150 * time.Millisecond
)

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

// windowRefreshInterval is how often the watcher re-probes local backends for
// their max_model_len. Slow: a model's context length changes only when it is
// relaunched, so a frequent probe would be wasted cross-fabric GETs.
// windowProbeTimeout bounds a single backend probe.
const (
	windowRefreshInterval = 60 * time.Second
	windowProbeTimeout    = 5 * time.Second
)

type router struct {
	path        string // config path to watch ("" disables hot-reload)
	boundListen string // immutable effective listener; hot reload cannot rebind the server
	snap        atomic.Pointer[snapshot]
	// fleet holds models discovered from SparkFleet (fleet.go), refreshed by the
	// watcher on fleetRefreshInterval. Separate from snap because it refreshes on
	// its own cadence (HTTP poll), independent of the config file's mtime. Never
	// nil after newRouter; lookup() consults it after configured models.
	fleet atomic.Pointer[map[string]modelEntry]
	// fleetState is the last-logged discovery state ("up:N" / "down"). Touched ONLY
	// by the watcher goroutine (the sole caller of refreshFleet), so it needs no
	// lock; it exists to log discovery on transitions instead of every 15s poll.
	fleetState string
	// secretsPath is wormhole's own secrets file (secrets.go), watched alongside the
	// config so a key edit hot-reloads with no restart. secretsMtime is the last-seen
	// modtime; like fleetState it is touched ONLY by the watcher goroutine
	// (reloadIfChanged), so it needs no lock.
	secretsPath  string
	secretsMtime int64
	// windows caches each LOCAL model's max_model_len, probed from its backend's
	// /v1/models by refreshWindows on the watch loop. Lock-free read in
	// listModels/status; never nil after newRouter. Empty for cloud/anthropic
	// models — max_model_len is a vLLM serving fact, not theirs. Surfacing it lets
	// a downstream client (the Deneb gateway, the native picker) discover a
	// wormhole-fronted model's context window without probing the backend directly.
	windows atomic.Pointer[map[string]int]
	// keyHealth caches each CLOUD model's last upstream-auth probe (keyhealth.go),
	// refreshed by refreshKeyHealth on the watch loop. Lock-free read in status;
	// never nil after newRouter. Empty for local (keyless) models. Surfacing it lets
	// the gateway's model picker show a dead/invalid cloud key before a request 401s.
	keyHealth atomic.Pointer[map[string]keyHealthState]
	metrics   *metrics // per-request counters, exposed at GET /metrics
	// usage is the persistent per-model token/request meter behind GET /v1/usage
	// (usage.go). Separate from metrics: metrics is in-memory Prometheus-shaped
	// observability, usage is month-windowed accounting that survives restarts.
	usage *usageMeter
	// circuits remembers repeated per-model transient failures across requests.
	// Open models move behind healthy fallbacks until their cooldown expires.
	circuits *circuitBook
	client   *http.Client
	log      *slog.Logger
}

func newRouter(cfg config, path string, log *slog.Logger) *router {
	rt := &router{
		path:        path,
		boundListen: cfg.Listen,
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
		log:      log,
		metrics:  newMetrics(),
		usage:    newUsageMeter(path),
		circuits: newCircuitBook(),
	}
	rt.snap.Store(buildSnapshot(cfg, time.Time{}))
	empty := map[string]modelEntry{}
	rt.fleet.Store(&empty)
	emptyWindows := map[string]int{}
	rt.windows.Store(&emptyWindows)
	emptyHealth := map[string]keyHealthState{}
	rt.keyHealth.Store(&emptyHealth)
	rt.secretsPath = secretsFileFor(path)
	rt.secretsMtime = secretsMtimeNanos(rt.secretsPath)
	return rt
}

// cur returns the live config snapshot (lock-free).
func (rt *router) cur() *snapshot { return rt.snap.Load() }

func (rt *router) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", rt.chatCompletions)
	mux.HandleFunc("POST /v1/messages", rt.messages)
	mux.HandleFunc("GET /v1/models", rt.listModels)
	mux.HandleFunc("GET /v1/usage", rt.usageHandler)
	mux.HandleFunc("GET /status", rt.status)
	mux.HandleFunc("GET /metrics", rt.metricsHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// authed gates a request on the wormhole token. An empty configured token means
// "open" (dev/loopback). main() and the hot-reload path enforce that an open
// configuration can only be used on an immutable loopback listener.
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
	client := identifyClient(r) // who is calling — for per-client shaping + metrics
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	w = sw
	model := ""
	defer func() {
		d := time.Since(start)
		rt.metrics.record(model, string(client.kind), sw.status, d)
		rt.log.Debug("request", "model", model, "client", client.name, "status", sw.status, "ms", d.Milliseconds())
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
		rt.serveAuto(client, w, r, body, proto, pathSuffix)
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
	rt.forward(client, w, r, entry, body, proto, pathSuffix)
}

// shapeFor rebuilds the upstream request body for one entry from the RAW
// client bytes: model rewrite, vision gate, and effort routing are all
// per-entry (capabilities and dialects differ), so a failover candidate must
// never inherit the primary's shaped bytes.
//
// Vision gate: image parts bound for a text-only upstream (GLM text models
// 400 on them and the failure repeats every later turn) are stripped to text
// stubs. No-op — byte-identical — for image-capable models and image-free
// requests (APC).
//
// Effort routing: X-Wormhole-No-Effort suppresses CLASSIFIER thinking routing
// (the gateway owns that and its prefix cache) but not a static "off" entry —
// the caller picked that variant by name, so no-thinking is its contract (see
// applyThinking). The cloud reasoning dialect runs regardless, since the
// gateway can't express it.
func (rt *router) shapeFor(entry modelEntry, body []byte, proto string, r *http.Request) []byte {
	out := body
	if entry.UpstreamModel != extractModel(body) {
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			out = rewritten
		}
	}
	if entry.Profile == profileKimi && proto == protocolAnthropic {
		out = applyKimiQuirks(out)
	}
	out = rt.applyVisionGate(entry, out, proto)
	if rt.cur().cfg.effortRoutingOn() {
		out = rt.applyThinking(entry, out, noEffortRouting(r))
		out = rt.applyReasoning(entry, out)
	}
	return out
}

// failoverChain returns the entry followed by its declared fallbacks (each
// entry's Fallback naming the next), capped at 3 candidates. A candidate must
// exist, speak the same protocol, and pass the local-only guard; a cycle or a
// guard failure ends the chain. Chain of one == no failover configured.
func (rt *router) failoverChain(primary modelEntry, proto string, r *http.Request) []modelEntry {
	chain := []modelEntry{primary}
	seen := map[string]bool{primary.Name: true}
	cur := primary
	for len(chain) < 3 {
		name := strings.TrimSpace(cur.Fallback)
		if name == "" || seen[name] {
			break
		}
		e, ok := rt.lookup(name)
		if !ok || e.protocol() != proto {
			break
		}
		if rt.localOnly(r) && !e.isLocal() {
			break
		}
		seen[name] = true
		chain = append(chain, e)
		cur = e
	}
	return chain
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
// order (local first), committing to the first non-transient response and
// falling through on an unreachable, 408, 429, or 5xx backend. Fallback only
// happens before any bytes are streamed; once a usable candidate starts
// responding we ride it out.
func (rt *router) serveAuto(client clientInfo, w http.ResponseWriter, r *http.Request, body []byte, proto, pathSuffix string) {
	cands := rt.circuits.order(rt.autoCandidates(r, proto))
	if len(cands) == 0 {
		writeErr(w, http.StatusServiceUnavailable, "no eligible auto model for this protocol/policy")
		return
	}
	var lastErr error
	for _, entry := range cands {
		// Same per-candidate shaping as the explicit route (capabilities and
		// dialects differ per entry, so each candidate reshapes the RAW bytes).
		out := rt.shapeFor(entry, body, proto, r)
		resp, err := rt.doUpstream(r, entry, out, pathSuffix)
		if err != nil {
			rt.recordCircuitFailure(entry.Name, false, 0)
			rt.log.Warn("auto: candidate unreachable, trying next", "model", entry.Name, "error", err)
			lastErr = err
			continue
		}
		if rt.observeCircuitResponse(entry.Name, resp) {
			rt.log.Warn("auto: candidate errored, trying next", "model", entry.Name, "status", resp.StatusCode)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("%s returned %d", entry.Name, resp.StatusCode)
			continue
		}
		rt.log.Info("auto routed", "model", entry.Name)
		rt.meterResponse(entry, resp)
		streamResponse(client, w, resp)
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
	// Entry-profile headers first, then auth/protocol pins — the pins win on
	// conflict so a headers block can never break authentication.
	for k, v := range entry.Headers {
		upReq.Header.Set(k, v)
	}
	applyUpstreamAuth(upReq, entry, r)
	return rt.client.Do(upReq)
}

// doUpstreamWithRetry calls doUpstream, retrying a transient failure — a
// connection error or a 5xx — up to maxUpstreamRetries times before returning.
// It's safe because doUpstream hasn't read the body yet, so nothing has streamed:
// a 5xx/connection failure means the upstream produced no usable completion, so a
// fresh attempt can't duplicate output. A <500 response (success or a 4xx the
// client should see) returns immediately. The request context cancels the wait.
func (rt *router) doUpstreamWithRetry(r *http.Request, entry modelEntry, body []byte, pathSuffix string) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt <= maxUpstreamRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(time.Duration(attempt) * retryBackoffBase):
			}
		}
		resp, err = rt.doUpstream(r, entry, body, pathSuffix)
		if err != nil {
			rt.log.Warn("upstream transient error, retrying", "model", entry.Name, "attempt", attempt+1, "error", err)
			continue
		}
		if resp.StatusCode >= 500 && attempt < maxUpstreamRetries {
			rt.log.Warn("upstream 5xx, retrying", "model", entry.Name, "attempt", attempt+1, "status", resp.StatusCode)
			_ = resp.Body.Close()
			continue
		}
		return resp, nil
	}
	return resp, err // retries exhausted: surface the last error (resp is nil)
}

// forward proxies a model request and streams the response back, with a
// bounded retry of transient upstream failures (this is Deneb's model hot
// path) and, when the entry declares a Fallback, failover down the chain once
// retries are exhausted — an unreachable upstream or one still returning a
// transient status after retries moves to the next candidate instead of
// surfacing the failure.
// Failover happens only before any bytes stream; each candidate shapes its
// own body from the raw client bytes.
func (rt *router) forward(client clientInfo, w http.ResponseWriter, r *http.Request, primary modelEntry, rawBody []byte, proto, pathSuffix string) {
	cands := rt.circuits.order(rt.failoverChain(primary, proto, r))
	for i, entry := range cands {
		body := rt.shapeFor(entry, rawBody, proto, r)
		resp, err := rt.doUpstreamWithRetry(r, entry, body, pathSuffix)
		if err != nil {
			rt.recordCircuitFailure(entry.Name, false, 0)
			rt.log.Warn("upstream call failed", "model", entry.Name, "url", entry.URL,
				"error", err, "fallbacksLeft", len(cands)-1-i)
			continue
		}
		if rt.observeCircuitResponse(entry.Name, resp) {
			if i < len(cands)-1 {
				rt.log.Warn("upstream transient failure, failing over",
					"model", entry.Name, "status", resp.StatusCode, "next", cands[i+1].Name)
				_ = resp.Body.Close()
				continue
			}
		}
		if entry.Name != primary.Name {
			// Warn, not Info: the primary the caller asked for is down — the
			// reply is rescued, but the operator should see the substitution.
			rt.log.Warn("failover routed", "from", primary.Name, "to", entry.Name)
		}
		rt.annotateProfileError(entry, resp)
		rt.commit(client, w, entry, body, resp)
		return
	}
	writeErr(w, http.StatusBadGateway, "upstream unreachable: "+primary.Name)
}

// annotateProfileError peeks a profile-entry error response to log a quirk
// diagnostic before the bytes stream to the client. Kimi's 400 messages are
// misleading on their own (internal "web:N" ordinals, blame far from the
// defect) — the hint saves the next diagnosis. Error bodies are small JSON,
// never SSE, so buffering a bounded prefix is safe; the body is reconstituted
// in place, byte-identical for the client, with the original Closer retained.
func (rt *router) annotateProfileError(entry modelEntry, resp *http.Response) {
	if entry.Profile != profileKimi || resp.StatusCode != http.StatusBadRequest {
		return
	}
	peek, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	rest := resp.Body
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(peek), rest), rest}
	if hint := kimiBadRequestHint(peek); hint != "" {
		rt.log.Warn("kimi 400 with known quirk signature",
			"model", entry.Name, "hint", hint, "error", string(peek))
	}
}

func (rt *router) recordCircuitFailure(name string, immediate bool, retryHint time.Duration) {
	view, opened := rt.circuits.recordFailure(name, immediate, retryHint)
	if opened {
		rt.log.Warn("model circuit opened", "model", name, "failures", view.Failures,
			"retryAfterMs", view.RetryAfterMS)
	}
}

// observeCircuitResponse keeps response classification, Retry-After handling,
// and recovery identical on the auto and explicit routing paths. It reports
// whether the response is transient and should move to the next candidate.
func (rt *router) observeCircuitResponse(name string, resp *http.Response) bool {
	failure, immediate := circuitFailureStatus(resp.StatusCode)
	if !failure {
		rt.recordCircuitSuccess(name)
		return false
	}
	hint := retryAfterHint(resp.Header.Get("Retry-After"), rt.circuits.now())
	rt.recordCircuitFailure(name, immediate, hint)
	return true
}

func (rt *router) recordCircuitSuccess(name string) {
	if rt.circuits.recordSuccess(name) {
		rt.log.Info("model circuit recovered", "model", name)
	}
}

// commit meters and streams one upstream response to the client — the point of
// no return after routing/failover has settled on an entry.
func (rt *router) commit(client clientInfo, w http.ResponseWriter, entry modelEntry, body []byte, resp *http.Response) {
	rt.meterResponse(entry, resp)
	// Diagnostic tap: WORMHOLE_DUMP_MODEL=<name> logs the exact request body
	// and the exact upstream response (head+tail) for that model's
	// NON-STREAMING calls. Off by default (empty env — zero hot-path cost);
	// used to capture what a client REALLY sends when curl reproductions and
	// live behavior diverge (the 2026-07-04 evolver truncation hunt).
	if dump := os.Getenv("WORMHOLE_DUMP_MODEL"); dump != "" && dump == entry.Name &&
		!bytes.Contains(body, []byte(`"stream":true`)) {
		rt.log.Info("dump: request", "model", entry.Name, "len", len(body),
			"head", dumpSlice(body, 500, false), "tail", dumpSlice(body, 300, true))
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if rerr != nil {
			writeErr(w, http.StatusBadGateway, "upstream read failed: "+entry.Name)
			return
		}
		rt.log.Info("dump: response", "model", entry.Name, "status", resp.StatusCode, "len", len(data),
			"head", dumpSlice(data, 500, false), "tail", dumpSlice(data, 700, true))
		for k, vs := range resp.Header {
			if strings.EqualFold(k, "Content-Length") {
				continue // length may differ after buffering; let net/http set it
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(data)
		return
	}
	streamResponse(client, w, resp)
}

// meterResponse wraps the upstream response body with the usage tail tee so
// token counts land in the /v1/usage meter when the stream finishes. The bytes
// reaching the client are untouched (read-through copy of a bounded tail).
func (rt *router) meterResponse(entry modelEntry, resp *http.Response) {
	name := entry.Name
	resp.Body = newUsageTail(resp.Body, func(tail []byte) {
		in, out := parseUsageTail(tail)
		rt.usage.record(name, in, out)
		rt.usage.maybeFlush()
	})
}

// dumpSlice returns the head (or tail) n bytes of b as a string for logging.
func dumpSlice(b []byte, n int, tail bool) string {
	if len(b) <= n {
		return string(b)
	}
	if tail {
		return "…" + string(b[len(b)-n:])
	}
	return string(b[:n]) + "…"
}

// streamResponse copies the upstream status, headers, and body straight back —
// flushing as chunks arrive so SSE tokens reach the client immediately. The
// caller's response shaper (shaper.go, keyed off the identified client) gets to
// adjust headers and wrap the body stream; every client gets the zero-overhead
// identityShaper today, so this stays a faithful pass-through.
func streamResponse(client clientInfo, w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	shaper := shaperFor(client)

	// Copy upstream headers (Content-Type drives SSE vs JSON on the client side),
	// then let the shaper adjust them before they're committed by WriteHeader.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	shaper.header(w.Header())
	w.WriteHeader(resp.StatusCode)

	// The shaper wraps the upstream body (identity returns it unchanged). We still
	// drive the read/flush loop here so streaming + flush-per-chunk behaviour is
	// identical no matter which shaper is in play.
	src := shaper.body(resp.Body)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, rerr := src.Read(buf)
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
		// MaxModelLen mirrors the backend's vLLM context length so a discovering
		// client (the Deneb gateway, the native picker) gets the window from this
		// front instead of probing the backend directly. Omitted for cloud models.
		MaxModelLen int `json:"max_model_len,omitempty"`
	}
	models := rt.mergedModels() // configured + SparkFleet-discovered
	windows := rt.windows.Load()
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
		row := modelRow{ID: e.Name, Object: "model", OwnedBy: owner}
		if windows != nil {
			row.MaxModelLen = (*windows)[e.Name]
		}
		rows = append(rows, row)
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
