package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// maxBodyBytes caps an inbound request body. Long-context chats are large but
// bounded; this stops a runaway client from buffering us out of memory.
const maxBodyBytes = 32 << 20 // 32 MiB

// router fans /v1 requests out to upstream backends by model name.
type router struct {
	cfg    config
	models map[string]modelEntry
	client *http.Client
	log    *slog.Logger
}

func newRouter(cfg config, log *slog.Logger) *router {
	m := make(map[string]modelEntry, len(cfg.Models))
	for _, e := range cfg.Models {
		m[e.Name] = e
	}
	return &router{
		cfg:    cfg,
		models: m,
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
		log: log,
	}
}

func (rt *router) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", rt.chatCompletions)
	mux.HandleFunc("POST /v1/messages", rt.messages)
	mux.HandleFunc("GET /v1/models", rt.listModels)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// authed gates a request on the wormhole token. An empty configured token means
// "open" (dev/loopback) — main() warns loudly about that at boot.
func (rt *router) authed(w http.ResponseWriter, r *http.Request) bool {
	if rt.cfg.Token == "" {
		return true
	}
	if clientToken(r) != rt.cfg.Token {
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
	if !rt.authed(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read request body")
		return
	}
	model := extractModel(body)
	if model == "" {
		writeErr(w, http.StatusBadRequest, "missing 'model'")
		return
	}
	// "auto" (when configured) lets the client delegate the choice.
	if model == rt.autoName() && len(rt.cfg.Auto) > 0 {
		rt.serveAuto(w, r, body, proto, pathSuffix)
		return
	}
	entry, ok := rt.models[model]
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
	out = rt.applyThinking(entry, out)
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
		out = rt.applyThinking(entry, out)
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
	out := make([]modelEntry, 0, len(rt.cfg.Auto))
	for _, name := range rt.cfg.Auto {
		e, ok := rt.models[name]
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
	if rt.cfg.AutoName != "" {
		return rt.cfg.AutoName
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
	rows := make([]modelRow, 0, len(rt.cfg.Models)+1)
	// Advertise the reserved auto name first so clients see they can delegate.
	if len(rt.cfg.Auto) > 0 {
		rows = append(rows, modelRow{ID: rt.autoName(), Object: "model", OwnedBy: "wormhole-auto"})
	}
	for _, e := range rt.cfg.Models {
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
