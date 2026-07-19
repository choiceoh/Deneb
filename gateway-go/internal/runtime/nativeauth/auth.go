// Package nativeauth authenticates standalone native-client HTTP requests.
package nativeauth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Authenticate verifies the native client token carried in the request header.
func Authenticate(w http.ResponseWriter, r *http.Request, logger *slog.Logger) (*clientauth.Identity, bool) {
	token := strings.TrimSpace(r.Header.Get(clientauth.Header))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing client token"}, logger)
		return nil, false
	}
	if !clientauth.Verify(token) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid client token"}, logger)
		return nil, false
	}
	return syntheticOperatorIdentity(), true
}

// AuthenticateDownload verifies a native client token carried in the query string.
func AuthenticateDownload(w http.ResponseWriter, r *http.Request, logger *slog.Logger) (*clientauth.Identity, bool) {
	token := strings.TrimSpace(r.URL.Query().Get("clientToken"))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing client token"}, logger)
		return nil, false
	}
	if !clientauth.Verify(token) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid client token"}, logger)
		return nil, false
	}
	return syntheticOperatorIdentity(), true
}

func writeJSON(w http.ResponseWriter, status int, value any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && logger != nil {
		httputil.LogEncodeError(logger, "native auth: json encode error", err)
	}
}
