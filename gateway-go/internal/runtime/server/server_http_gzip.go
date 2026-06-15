// server_http_gzip.go — negotiated gzip for the miniapp RPC JSON responses.
//
// Compression is strictly negotiated: the server gzips only when the request
// advertised gzip in Accept-Encoding, so a client always receives a body it can
// decode (there is no mismatched/undecodable case). The native client's Android
// OkHttp engine adds Accept-Encoding: gzip and transparently decompresses, so it
// gets compression for free; engines that don't advertise it (CIO/Darwin) simply
// receive the plain body. Mobile-over-cellular is the win — list/detail JSON
// (mail, transcripts, wiki, search) compresses ~5-10x.
//
// Scope: ONLY the RPC JSON response. The SSE stream/events endpoints are separate
// handlers and must never be gzipped (it would buffer and break token streaming).
package server

import (
	"compress/gzip"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// gzipMinBytes — below this, gzip's framing overhead + CPU isn't worth it.
const gzipMinBytes = 1024

// clientAcceptsGzip reports whether the request advertised gzip in Accept-Encoding.
func clientAcceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		token := strings.TrimSpace(part)
		if i := strings.IndexByte(token, ';'); i >= 0 { // strip ";q=..."
			token = strings.TrimSpace(token[:i])
		}
		if strings.EqualFold(token, "gzip") {
			return true
		}
	}
	return false
}

// writeRPCJSON marshals v as JSON and writes it, gzip-compressing when the client
// accepts gzip and the body is large enough to be worth it. The caller must have
// already set Content-Type (and any other headers) — this commits the response.
// Encode/write errors are logged, not surfaced (the body write has begun).
func writeRPCJSON(w http.ResponseWriter, r *http.Request, v any, logger *slog.Logger, method string) {
	data, err := json.Marshal(v)
	if err != nil {
		if logger != nil {
			logger.Error("miniapp rpc encode response", "method", method, "error", err)
		}
		return
	}
	if clientAcceptsGzip(r) && len(data) >= gzipMinBytes {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		if _, err := gz.Write(data); err != nil && logger != nil {
			logger.Error("miniapp rpc gzip write", "method", method, "error", err)
		}
		if err := gz.Close(); err != nil && logger != nil {
			logger.Error("miniapp rpc gzip close", "method", method, "error", err)
		}
		return
	}
	if _, err := w.Write(data); err != nil && logger != nil {
		logger.Error("miniapp rpc write response", "method", method, "error", err)
	}
}
