package fleetapi

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// AlertGate is the narrow cooldown contract required by the Fleet webhook.
// The server supplies its process-wide proactive gate so Fleet and watchdog
// alerts share one deduplication timeline.
type AlertGate interface {
	ShouldRelay(title, level string, now time.Time) bool
}

// AlertHookConfig contains the Fleet webhook's transport boundaries.
type AlertHookConfig struct {
	Gate    AlertGate
	Publish func(title, body string)
	Logger  *slog.Logger
}

// AlertHook receives SparkFleet's generic webhook dialect and emits a native
// operational alert. It is loopback-only because SparkFleet posts from the
// same host and the gateway may be configured to bind a wider interface.
type AlertHook struct {
	gate    AlertGate
	publish func(title, body string)
	logger  *slog.Logger
}

// NewAlertHook constructs the Fleet webhook adapter. Gate and Publish may be
// nil for lightweight diagnostics; a nil gate relays and a nil publisher is a
// successful no-op, matching the gateway's former nil-safe behavior.
func NewAlertHook(cfg AlertHookConfig) *AlertHook {
	return &AlertHook{gate: cfg.Gate, publish: cfg.Publish, logger: cfg.Logger}
}

type alertEvent struct {
	Source  string `json:"source"`
	Level   string `json:"level"` // ok | warn | bad
	Title   string `json:"title"`
	Message string `json:"message"`
}

// ServeHTTP validates, deduplicates, and publishes one Fleet alert.
func (h *AlertHook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		h.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	var event alertEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(event.Title) == "" && strings.TrimSpace(event.Message) == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "title or message required"})
		return
	}
	if h.gate != nil && !h.gate.ShouldRelay(event.Title, event.Level, time.Now()) {
		h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "suppressed": true})
		return
	}

	badge := "🛰"
	switch event.Level {
	case "bad":
		badge = "🔴"
	case "warn":
		badge = "⚠️"
	}
	title := strings.TrimSpace(badge + " 플릿 · " + event.Title)
	if h.publish != nil {
		h.publish(title, event.Message)
	}
	h.loggerOrDefault().Info("fleet alert relayed to clients", "level", event.Level, "title", event.Title)
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AlertHook) loggerOrDefault() *slog.Logger {
	if h != nil && h.logger != nil {
		return h.logger
	}
	return slog.Default()
}

func (h *AlertHook) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		httputil.LogEncodeError(h.loggerOrDefault(), "fleet alert: json encode error", err)
	}
}

func isLoopbackRemote(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
