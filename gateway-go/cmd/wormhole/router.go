package main

import (
	"encoding/json"
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

// resolve runs the shared front-of-house for both protocol endpoints: auth, read
// the body, look up the model, apply the egress guard, and rewrite the model id
// for the upstream. On any failure it writes the error response and returns
// ok=false. The OpenAI and Anthropic request bodies both carry a top-level
// "model" field, so this is protocol-agnostic.
func (rt *router) resolve(w http.ResponseWriter, r *http.Request) (modelEntry, []byte, bool) {
	if !rt.authed(w, r) {
		return modelEntry{}, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read request body")
		return modelEntry{}, nil, false
	}
	model := extractModel(body)
	if model == "" {
		writeErr(w, http.StatusBadRequest, "missing 'model'")
		return modelEntry{}, nil, false
	}
	entry, ok := rt.models[model]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown model: "+model)
		return modelEntry{}, nil, false
	}
	// Local-first egress guard: a local-only request (instance mode or the
	// X-Wormhole-Local-Only header) must not reach a cloud-backed model.
	if rt.localOnly(r) && !entry.isLocal() {
		writeErr(w, http.StatusForbidden, "model '"+model+"' is cloud-backed and blocked by local-only policy")
		return modelEntry{}, nil, false
	}
	out := body
	if entry.UpstreamModel != model {
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			out = rewritten
		}
	}
	return entry, out, true
}

// chatCompletions serves OpenAI clients: POST /v1/chat/completions → an
// OpenAI-protocol backend's /chat/completions.
func (rt *router) chatCompletions(w http.ResponseWriter, r *http.Request) {
	entry, out, ok := rt.resolve(w, r)
	if !ok {
		return
	}
	if entry.protocol() != protocolOpenAI {
		writeErr(w, http.StatusBadRequest, "model '"+entry.Name+"' speaks the anthropic protocol — use POST /v1/messages")
		return
	}
	rt.forward(w, r, entry, out, "/chat/completions")
}

// messages serves Anthropic clients: POST /v1/messages → an Anthropic-protocol
// backend's /messages. No translation — the client already speaks Anthropic, so
// the request rides straight through (auth header swapped, model rewritten).
func (rt *router) messages(w http.ResponseWriter, r *http.Request) {
	entry, out, ok := rt.resolve(w, r)
	if !ok {
		return
	}
	if entry.protocol() != protocolAnthropic {
		writeErr(w, http.StatusBadRequest, "model '"+entry.Name+"' speaks the openai protocol — use POST /v1/chat/completions")
		return
	}
	rt.forward(w, r, entry, out, "/messages")
}

// forward proxies the (possibly model-rewritten) request to the upstream at
// pathSuffix and streams the response straight back — status, headers, and body
// bytes — flushing as chunks arrive so SSE tokens reach the client immediately.
// The upstream key is injected here (protocol-aware); the client never sees it.
func (rt *router) forward(w http.ResponseWriter, r *http.Request, entry modelEntry, body []byte, pathSuffix string) {
	url := strings.TrimRight(entry.URL, "/") + pathSuffix
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "build upstream request")
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(upReq, entry, r)

	resp, err := rt.client.Do(upReq)
	if err != nil {
		rt.log.Warn("upstream call failed", "model", entry.Name, "url", entry.URL, "error", err)
		writeErr(w, http.StatusBadGateway, "upstream unreachable: "+entry.Name)
		return
	}
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
	rows := make([]modelRow, 0, len(rt.cfg.Models))
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
