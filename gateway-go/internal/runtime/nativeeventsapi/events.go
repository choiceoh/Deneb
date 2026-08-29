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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
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

// GatewayBroadcaster is the slice of *events.Broadcaster this adapter needs to
// attach a desktop connection to the gateway event plane (agent.event frames
// for the spectate surface). Session-scoped delivery itself is driven by the
// events RPC surface (sessions.messages.subscribe) against the same connID.
type GatewayBroadcaster interface {
	Subscribe(sub events.Subscriber, filter events.Filter)
	Unsubscribe(id string)
}

// Config supplies the event stream adapter's gateway-owned dependencies.
type Config struct {
	PushHub         PushHub
	ShutdownContext context.Context
	Logger          *slog.Logger
	// Broadcaster is optional. When set, DESKTOP-kind connections are also
	// registered on the gateway event broadcaster: the stream opens with an
	// `event: hello` frame carrying the connection's connId, and targeted
	// gateway events (agent.event) arrive as `event: gateway` frames. Phone
	// connections are left untouched — their daemon only understands `push`.
	Broadcaster GatewayBroadcaster
}

// Handler serves authenticated native-client event streams.
type Handler struct {
	pushHub         PushHub
	shutdownContext context.Context
	logger          *slog.Logger
	broadcaster     GatewayBroadcaster
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
		broadcaster:     cfg.Broadcaster,
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
	pushEvents, unsubscribe := h.pushHub.Subscribe(kind)
	defer unsubscribe()
	if h.logger != nil {
		h.logger.Info("native client events stream opened", "subscribers", h.pushHub.SubscriberCount())
	}

	// Desktop connections also join the gateway event plane. The broadcaster
	// side of this pipeline (subscription RPCs, targeted agent.event emission,
	// per-run sequencing) survived the TS-gateway retirement fully wired but
	// with no transport serving its subscribers — every emit returned to zero
	// recipients. This attach is the missing half.
	var gwCh chan []byte
	if h.broadcaster != nil && kind == nativepush.KindDesktop {
		connID := "evt-" + randomConnID()
		gwCh = make(chan []byte, gatewayFrameBuffer)
		sub := &gatewayEventSubscriber{id: connID, ch: gwCh, logger: h.logger}
		h.broadcaster.Subscribe(sub, events.Filter{Events: map[string]struct{}{
			"agent.event": {},
		}})
		// Unsubscribe also clears any session subscriptions the client made
		// against this connID, so a dropped stream cannot leak recipients.
		defer h.broadcaster.Unsubscribe(connID)
		hello, _ := json.Marshal(map[string]string{"connId": connID})
		if _, err := io.WriteString(w, "event: hello\ndata: "+string(hello)+"\n\n"); err != nil {
			return
		}
		flusher.Flush()
	}

	// Stop when the client disconnects (request ctx) or the server shuts down.
	streamPushEvents(r.Context(), h.shutdownContext, w, flusher, pushEvents, gwCh)
}

// randomConnID returns 16 random bytes as hex — unique enough for a
// connection lifetime, no external uuid dependency.
func randomConnID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// gatewayFrameBuffer bounds a connection's queued gateway frames. Agent events
// are low-rate (a few per tool call); a full buffer means the consumer stopped
// reading and dropping is safer than stalling the broadcast fan-out.
const gatewayFrameBuffer = 64

// gatewayEventSubscriber adapts one SSE connection to events.Subscriber.
// SendEvent must never block (it runs inside broadcast fan-out), so it drops
// when the channel is full and counts the loss.
type gatewayEventSubscriber struct {
	id      string
	ch      chan []byte
	logger  *slog.Logger
	dropped atomic.Int64
}

func (s *gatewayEventSubscriber) ID() string            { return s.id }
func (s *gatewayEventSubscriber) IsAuthenticated() bool { return true }
func (s *gatewayEventSubscriber) BufferedAmount() int64 { return 0 }

func (s *gatewayEventSubscriber) SendEvent(data []byte) error {
	select {
	case s.ch <- data:
		return nil
	default:
		if s.dropped.Add(1) == 1 && s.logger != nil {
			s.logger.Warn("gateway event subscriber backlogged; dropping frames", "connId", s.id)
		}
		return nil
	}
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
	pushEvents <-chan nativepush.Event,
	gatewayFrames <-chan []byte, // nil for phone connections — the case never fires
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
		case ev, ok := <-pushEvents:
			if !ok {
				return
			}
			if !writeFrame("push", ev) {
				return
			}
		case frame := <-gatewayFrames:
			// Pre-marshaled protocol.EventFrame from the broadcaster — the
			// client reads frame.event/payload from the data itself.
			if _, err := io.WriteString(w, "event: gateway\ndata: "); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
