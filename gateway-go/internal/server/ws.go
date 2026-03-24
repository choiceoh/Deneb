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
	defer s.clients.Delete(client.connID)

	s.logger.Info("websocket connected", "connId", client.connID, "remote", r.RemoteAddr)

	// Handshake: first message must be a connect request.
	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), time.Duration(protocol.HandshakeTimeoutMs)*time.Millisecond)
	if err := s.handleHandshake(handshakeCtx, client); err != nil {
		handshakeCancel()
		s.logger.Warn("handshake failed", "connId", client.connID, "error", err)
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	handshakeCancel()

	conn.SetReadLimit(protocol.MaxPayloadBytes)

	// Enter message loop.
	s.runMessageLoop(r.Context(), client)
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

	if !protocol.ValidateProtocolVersion(&params) {
		errShape := protocol.NewError(protocol.ErrInvalidRequest,
			fmt.Sprintf("protocol version mismatch: server=%d, client range=%d-%d",
				protocol.ProtocolVersion, params.MinProtocol, params.MaxProtocol))
		s.writeFrame(client, protocol.NewResponseError(req.ID, errShape))
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

	return s.writeFrame(client, helloResp)
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
			s.writeFrame(client, resp)
		}
	}
}

func (s *Server) writeFrame(client *WsClient, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("marshal frame", "connId", client.connID, "error", err)
		return err
	}

	client.writeMu.Lock()
	defer client.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return client.conn.Write(ctx, websocket.MessageText, data)
}
