// server_http_miniapp_events.go — proactive-event SSE stream for the native client.
//
//	GET /api/v1/miniapp/events
//	  X-Deneb-Client-Token: <token>   (or Authorization: tma <initData>)
//
// The native app's foreground daemon holds this connection open and raises a
// local notification for each frame:
//
//	event: push       data: {"title":"...","body":"..."}   (a proactive report)
//	(: keepalive comments during silent stretches)
//
// Auth mirrors the other miniapp endpoints. The stream lives until the client
// disconnects or the server shuts down. See client_push.go for the hub and
// proactive_relay.go for where frames originate.
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// clientEventsKeepaliveInterval keeps intermediaries (cloudflared/nginx) and the
// phone's connection from idling out during long silent stretches between
// proactive reports.
const clientEventsKeepaliveInterval = 30 * time.Second

func (s *Server) handleMiniappEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappRequest(w, r); !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if s.pushHub == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "push hub not ready"})
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Server", "deneb-gateway")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, unsubscribe := s.pushHub.subscribe()
	defer unsubscribe()
	if s.logger != nil {
		s.logger.Info("native client events stream opened", "subscribers", s.pushHub.subscriberCount())
	}

	// Stop when the client disconnects (request ctx) or the server shuts down.
	streamPushEvents(r.Context(), s.ShutdownCtx(), w, flusher, events)
}

// streamPushEvents writes push frames (and periodic keepalive comments) to an
// already-open SSE response until either context fires or the events channel
// closes. Split out from the handler so it can be unit-tested without server
// auth / lifecycle wiring.
func streamPushEvents(
	clientCtx, shutdownCtx context.Context,
	w io.Writer,
	flusher http.Flusher,
	events <-chan clientPushEvent,
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
