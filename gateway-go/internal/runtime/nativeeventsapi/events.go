// Package nativeeventsapi serves the native-client proactive-event SSE stream.
//
//	GET /api/v1/miniapp/events
//	  X-Deneb-Client-Token: <token>
//
// The native app's foreground daemon holds this connection open and raises a
// local notification for each frame:
//
//	event: push       data: {"title":"...","body":"..."}   (a proactive report)
//	(: keepalive comments during silent stretches)
//
// Auth mirrors the other miniapp endpoints. The stream lives until the client
// disconnects or the server shuts down. See runtime/nativepush for the hub and
// proactive_relay.go for one major producer.
package nativeeventsapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// ClientKindHeader tags a client surface (e.g. "desktop") on SSE subscribe
// requests. Browser-context clients (Tauri webview, vite dev) send it from a
// CORS-enforced origin, so it must stay in the CORS allow-list (routes.go).
const ClientKindHeader = "X-Deneb-Client-Kind"

// PushHub is the event-stream adapter's complete fan-out dependency.
type PushHub interface {
	Subscribe(kind nativepush.ClientKind) (<-chan nativepush.Event, func())
	SubscriberCount() int
}

// Config supplies the event stream adapter's gateway-owned dependencies.
type Config struct {
	PushHub         PushHub
	ShutdownContext context.Context
	Logger          *slog.Logger
}

// Handler serves authenticated native-client event streams.
type Handler struct {
	pushHub         PushHub
	shutdownContext context.Context
	logger          *slog.Logger
}

// New creates a native-client event stream handler.
func New(cfg Config) *Handler {
	shutdownContext := cfg.ShutdownContext
	if shutdownContext == nil {
		shutdownContext = context.Background()
	}
	return &Handler{
		pushHub:         cfg.PushHub,
		shutdownContext: shutdownContext,
		logger:          cfg.Logger,
	}
}

// Events serves the authenticated native-client server-sent event stream.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.Authenticate(w, r, h.logger); !ok {
		return
	}
	// This SSE stream lives until the client disconnects (potentially hours), so
	// lift the global WriteTimeout for it.
	disableWriteDeadline(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if h.pushHub == nil {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "push hub not ready"})
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	header.Set("Server", "deneb-gateway")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	kind := nativepush.ClientKindFromHeader(r.Header.Get(ClientKindHeader))
	events, unsubscribe := h.pushHub.Subscribe(kind)
	defer unsubscribe()
	if h.logger != nil {
		h.logger.Info("native client events stream opened", "subscribers", h.pushHub.SubscriberCount())
	}

	// Stop when the client disconnects (request ctx) or the server shuts down.
	streamPushEvents(r.Context(), h.shutdownContext, w, flusher, events)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && h.logger != nil {
		httputil.LogEncodeError(h.logger, "native events api: json encode error", err)
	}
}

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}

// clientEventsKeepaliveInterval keeps intermediaries (cloudflared/nginx) and the
// phone's connection from idling out during long silent stretches between
// proactive reports.
const clientEventsKeepaliveInterval = 30 * time.Second

// streamPushEvents writes push frames (and periodic keepalive comments) to an
// already-open SSE response until either context fires or the events channel
// closes. Split out from the handler so it can be unit-tested without server
// auth / lifecycle wiring.
func streamPushEvents(
	clientCtx, shutdownCtx context.Context,
	w io.Writer,
	flusher http.Flusher,
	events <-chan nativepush.Event,
) {
	writeFrame := func(event string, payload any) bool {
		if _, err := io.WriteString(w, "event: "+event+"\n"); err != nil {
			return false
		}
		data, err := json.Marshal(payload)
		if err != nil {
			data = []byte("{}")
		}
		_, _ = io.WriteString(w, "data: ")
		if _, err := w.Write(data); err != nil {
			return false
		}
		if _, err := io.WriteString(w, "\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	ticker := time.NewTicker(clientEventsKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-clientCtx.Done():
			return
		case <-shutdownCtx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if !writeFrame("push", ev) {
				return
			}
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
