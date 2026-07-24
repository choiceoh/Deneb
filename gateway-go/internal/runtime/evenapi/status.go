package evenapi

import "net/http"

// Status handles GET /api/even/status — lightweight readiness for the plugin
// setup screen (auth required; never returns the token).
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "even g2 bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled (set "+EnvBridgeToken+")")
		return
	}
	presented := ParseBearer(r.Header.Get("Authorization"))
	if !tokenMatch(h.token, presented) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	chatReady := h.chat != nil && h.chat.ChatReady()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"enabled":   true,
		"session":   h.session,
		"chatReady": chatReady,
		"glance":    true,
	})
}
