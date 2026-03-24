package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

// WsClient represents a connected WebSocket client.
type WsClient struct {
	conn    *websocket.Conn
	connID  string
	created time.Time
	role    string
	authed  bool
	writeMu sync.Mutex
}

// handleWsUpgrade upgrades an HTTP connection to WebSocket and manages the lifecycle.
func (s *Server) handleWsUpgrade(w http.ResponseWriter, r *http.Request) {
	// Enforce connection limit.
	if s.clientCnt.Load() >= maxWebSocketClients {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin validation deferred
	})
	if err != nil {
		s.logger.Error("websocket accept failed", "error", err)
		return
	}

	conn.SetReadLimit(protocol.MaxPreAuthPayloadBytes)

	client := &WsClient{
		conn:    conn,
		connID:  fmt.Sprintf("conn-%d", time.Now().UnixNano()),
		created: time.Now(),
	}

	s.clients.Store(client.connID, client)
	s.clientCnt.Add(1)
	defer func() {
		s.clients.Delete(client.connID)
		s.clientCnt.Add(-1)
	}()

	s.logger.Info("websocket connected", "connId", client.connID, "remote", r.RemoteAddr)

	// Handshake: first message must be a connect request.
	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), time.Duration(protocol.HandshakeTimeoutMs)*time.Millisecond)
	if err := s.handleHandshake(handshakeCtx, client); err != nil {
		handshakeCancel()
		s.logger.Warn("handshake failed", "connId", client.connID, "error", err)
		conn.Close(websocket.StatusPolicyViolation, "handshake failed")
		return
	}
	handshakeCancel()

	conn.SetReadLimit(protocol.MaxPayloadBytes)

	// Start tick heartbeat in background.
	tickCtx, tickCancel := context.WithCancel(r.Context())
	go s.tickLoop(tickCtx, client)

	// Enter message loop (blocks until disconnect).
	s.runMessageLoop(r.Context(), client)
	tickCancel()

	s.logger.Info("websocket disconnected", "connId", client.connID)
}

// handleHandshake reads the first frame and validates it as a connect request.
func (s *Server) handleHandshake(ctx context.Context, client *WsClient) error {
	_, data, err := client.conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read connect frame: %w", err)
	}

	var req protocol.RequestFrame
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("parse connect frame: %w", err)
	}

	if req.Type != protocol.FrameTypeRequest || req.Method != "connect" {
		return fmt.Errorf("expected connect request, got type=%q method=%q", req.Type, req.Method)
	}

	var params protocol.ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return fmt.Errorf("parse connect params: %w", err)
	}

	if err := protocol.ValidateConnectParams(&params); err != nil {
		return fmt.Errorf("invalid connect params: %w", err)
	}

	if !protocol.ValidateProtocolVersion(&params) {
		errShape := protocol.NewError(protocol.ErrInvalidRequest,
			fmt.Sprintf("protocol version mismatch: server=%d, client range=%d-%d",
				protocol.ProtocolVersion, params.MinProtocol, params.MaxProtocol))
		if err := s.writeFrame(ctx, client, protocol.NewResponseError(req.ID, errShape)); err != nil {
			s.logger.Error("failed to send protocol error", "connId", client.connID, "error", err)
		}
		return fmt.Errorf("protocol version mismatch")
	}

	hello := s.buildHelloOk(client)
	helloResp, err := protocol.NewResponseOK(req.ID, hello)
	if err != nil {
		return fmt.Errorf("build hello-ok: %w", err)
	}

	client.authed = true
	client.role = params.Role
	if client.role == "" {
		client.role = "operator"
	}

	return s.writeFrame(ctx, client, helloResp)
}

func (s *Server) buildHelloOk(client *WsClient) *protocol.HelloOk {
	return &protocol.HelloOk{
		Type:     "hello-ok",
		Protocol: protocol.ProtocolVersion,
		Server:   protocol.HelloServer{Version: s.version, ConnID: client.connID},
		Features: protocol.HelloFeatures{
			Methods: s.dispatcher.Methods(),
			Events:  []string{"tick", "agent.event", "shutdown"},
		},
		Snapshot: protocol.Snapshot{},
		Policy: protocol.HelloPolicy{
			MaxPayload:       protocol.MaxPayloadBytes,
			MaxBufferedBytes: protocol.MaxBufferedBytes,
			TickIntervalMs:   protocol.TickIntervalMs,
		},
	}
}

// runMessageLoop reads frames from the WebSocket and dispatches them.
func (s *Server) runMessageLoop(ctx context.Context, client *WsClient) {
	for {
		_, data, err := client.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || ctx.Err() != nil {
				return
			}
			s.logger.Error("websocket read error", "connId", client.connID, "error", err)
			return
		}

		frameType, err := protocol.ParseFrameType(data)
		if err != nil {
			s.logger.Warn("invalid frame", "connId", client.connID, "error", err)
			continue
		}

		if frameType == protocol.FrameTypeRequest {
			var req protocol.RequestFrame
			if err := json.Unmarshal(data, &req); err != nil {
				s.logger.Warn("unmarshal request", "connId", client.connID, "error", err)
				continue
			}
			resp := s.dispatcher.Dispatch(ctx, &req)
			if err := s.writeFrame(ctx, client, resp); err != nil {
				s.logger.Warn("response write failed", "connId", client.connID, "method", req.Method, "error", err)
				return
			}
		}
	}
}

// tickLoop sends periodic tick events to the client so both sides can detect
// dead connections. Mirrors TICK_INTERVAL_MS from src/gateway/server-constants.ts.
func (s *Server) tickLoop(ctx context.Context, client *WsClient) {
	ticker := time.NewTicker(time.Duration(protocol.TickIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ev, _ := protocol.NewEventFrame("tick", map[string]int64{
				"ts": time.Now().UnixMilli(),
			})
			if err := s.writeFrame(ctx, client, ev); err != nil {
				// Write failure means connection is dead; exit silently.
				return
			}
		}
	}
}

// writeFrame serializes v as JSON and writes it to the WebSocket connection.
// The write is bounded by a 5-second timeout derived from the parent context.
func (s *Server) writeFrame(ctx context.Context, client *WsClient, v any) error {
	if client.conn == nil {
		return fmt.Errorf("connection closed")
	}

	data, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("marshal frame", "connId", client.connID, "error", err)
		return err
	}

	client.writeMu.Lock()
	defer client.writeMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return client.conn.Write(writeCtx, websocket.MessageText, data)
}
