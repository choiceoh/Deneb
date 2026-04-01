package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

// runMessageLoop reads frames from the WebSocket and dispatches them.
// Uses single-pass unmarshal: tries RequestFrame first (the common case),
// falling back to type-peek only for non-request frames.
func (s *Server) runMessageLoop(ctx context.Context, client *WsClient) {
	var consecutiveBadFrames int
	for {
		_, data, err := client.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || ctx.Err() != nil {
				return
			}
			s.logger.Error("websocket read error", "connId", client.connID, "error", err)
			return
		}

		// Wire I/O: count inbound WebSocket frames.
		metrics.WireCallsTotal.Inc("ws_recv", "ok")
		metrics.WireBytesTotal.Add(int64(len(data)), "ws_recv", "in")

		// Track activity on every inbound WS message.
		client.touchActivity()
		if s.activity != nil {
			s.activity.Touch()
		}

		// Single-pass: unmarshal directly as RequestFrame (dominant case).
		var req protocol.RequestFrame
		if err := json.Unmarshal(data, &req); err != nil {
			consecutiveBadFrames++
			s.logger.Warn("unmarshal frame", "connId", client.connID, "error", err, "consecutive", consecutiveBadFrames)
			if consecutiveBadFrames >= maxConsecutiveBadFrames {
				s.logger.Warn("too many bad frames, closing connection", "connId", client.connID)
				return
			}
			// Send error response so the client knows the frame was rejected.
			errResp := protocol.NewResponseError("", protocol.NewError(
				protocol.ErrInvalidRequest, "malformed JSON frame"))
			if writeErr := s.writeFrame(ctx, client, errResp); writeErr != nil {
				return
			}
			continue
		}
		consecutiveBadFrames = 0 // reset on valid frame

		if req.Type != protocol.FrameTypeRequest {
			// Non-request frames (events, etc.) are ignored on inbound WS.
			continue
		}

		if req.Method == "" || req.ID == "" {
			s.logger.Warn("request missing method/id", "connId", client.connID)
			continue
		}

		// Authorize: check scope-based permissions.
		if authErr := rpc.AuthorizeMethod(req.Method, client.role, client.authed, client.scopes); authErr != nil {
			if err := s.writeFrame(ctx, client, protocol.NewResponseError(req.ID, authErr)); err != nil {
				return
			}
			continue
		}

		// Deduplicate: reject requests with recently-seen IDs.
		if !s.dedupe.Check(req.ID) {
			s.logger.Debug("duplicate request", "connId", client.connID, "id", req.ID)
			errResp := protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrConflict, "duplicate request ID"))
			if err := s.writeFrame(ctx, client, errResp); err != nil {
				return
			}
			continue
		}

		// Dispatch with a per-request timeout to prevent stuck handlers.
		dispatchCtx, dispatchCancel := context.WithTimeout(ctx, timeouts.RPCDispatch)
		resp := s.dispatcher.Dispatch(dispatchCtx, &req)
		dispatchCancel()

		if err := s.writeFrame(ctx, client, resp); err != nil {
			s.logger.Warn("response write failed", "connId", client.connID, "method", req.Method, "error", err)
			return
		}
	}
}

// runPingLoop sends periodic WebSocket pings and disconnects idle or unresponsive clients.
// nhooyr.io/websocket handles pong responses automatically; Ping() blocks until pong arrives.
func (s *Server) runPingLoop(ctx context.Context, client *WsClient) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check idle timeout first.
			if client.idleDuration() > idleTimeout {
				s.logger.Info("closing idle connection", "connId", client.connID, "idle", client.idleDuration().String())
				client.conn.Close(websocket.StatusGoingAway, "idle timeout")
				return
			}
			// Send ping; failure means connection is dead.
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := client.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				s.logger.Info("ping failed, closing connection", "connId", client.connID, "error", err)
				client.conn.Close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

// startTickBroadcaster runs a single goroutine that broadcasts tick events
// to all authenticated WebSocket clients. This replaces the per-client
// tickLoop pattern, reducing goroutine count from O(clients) to O(1).
func (s *Server) startTickBroadcaster(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(protocol.TickIntervalMs) * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data := buildTickJSON(time.Now().UnixMilli())
				s.clients.Range(func(_, value any) bool {
					client := value.(*WsClient)
					if client.authed {
						// Best-effort tick: ignore write errors (client will disconnect naturally).
						writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
						s.writeFrameRaw(writeCtx, client, data)
						cancel()
					}
					return true
				})
			}
		}
	}()
}

// tickPrefix is the pre-serialized tick event envelope up to the timestamp value.
// Format: {"type":"event","event":"tick","payload":{"ts":<TIMESTAMP>}}
var tickPrefix = []byte(`{"type":"event","event":"tick","payload":{"ts":`)
var tickSuffix = []byte(`}}`)

// buildTickJSON constructs a tick event JSON without json.Marshal by
// concatenating the static prefix, the integer timestamp, and the suffix.
func buildTickJSON(tsMs int64) []byte {
	buf := make([]byte, 0, len(tickPrefix)+20+len(tickSuffix))
	buf = append(buf, tickPrefix...)
	buf = appendInt64(buf, tsMs)
	buf = append(buf, tickSuffix...)
	return buf
}

// appendInt64 appends the decimal representation of n to buf.
func appendInt64(buf []byte, n int64) []byte {
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	if n == 0 {
		return append(buf, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(buf, tmp[i:]...)
}
