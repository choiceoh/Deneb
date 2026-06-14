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
	if bearer(r) != rt.cfg.Token {
		writeErr(w, http.StatusUnauthorized, "invalid wormhole token")
		return false
	}
	return true
}

func (rt *router) chatCompletions(w http.ResponseWriter, r *http.Request) {
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
	entry, ok := rt.models[model]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown model: "+model)
		return
	}
	// Local-first egress guard: a local-only request (instance mode or the
	// X-Wormhole-Local-Only header) must not reach a cloud-backed model.
	if rt.localOnly(r) && !entry.isLocal() {
		writeErr(w, http.StatusForbidden, "model '"+model+"' is cloud-backed and blocked by local-only policy")
		return
	}
	out := body
	if entry.UpstreamModel != model {
		if rewritten, rerr := rewriteModel(body, entry.UpstreamModel); rerr == nil {
			out = rewritten
		}
	}
	rt.forward(w, r, entry, out)
}

// forward proxies the (possibly model-rewritten) request to the upstream and
// streams the response straight back — status, headers, and body bytes — flushing
// as chunks arrive so SSE tokens reach the client immediately. The upstream key
// is injected here; the client never sees it.
func (rt *router) forward(w http.ResponseWriter, r *http.Request, entry modelEntry, body []byte) {
	url := strings.TrimRight(entry.URL, "/") + "/chat/completions"
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "build upstream request")
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	if entry.Key != "" {
		upReq.Header.Set("Authorization", "Bearer "+entry.Key)
	}

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

func bearer(r *http.Request) string {
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
