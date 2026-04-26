package openaiapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerAuth wraps next with Authorization: Bearer enforcement. When
// AuthToken is empty the wrapper is a no-op so loopback dev runs
// without configuring a token. Mismatch returns the OpenAI error
// envelope with HTTP 401.
func (r *routes) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.deps.AuthToken == "" {
			next.ServeHTTP(w, req)
			return
		}
		token := bearerFromHeader(req.Header.Get("Authorization"))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "invalid_request_error", "missing bearer token")
			return
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(r.deps.AuthToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid_request_error", "invalid bearer token")
			return
		}
		next.ServeHTTP(w, req)
	})
}

func bearerFromHeader(h string) string {
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
