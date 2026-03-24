package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

const (
	// dispatchTimeout bounds how long a single RPC handler can run before
	// being canceled. Prevents a stuck handler from blocking the message loop.
	dispatchTimeout = 30 * time.Second
)

// jsonBufPool reduces GC pressure for writeFrame by reusing marshal buffers.
var jsonBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

// WsClient represents a connected WebSocket client.
// Implements events.Subscriber for event broadcasting.
type WsClient struct {
	conn           *websocket.Conn
	connID         string
	created        time.Time
	role           string
	authed         bool
	deviceID       string
	scopes         []auth.Scope
	writeMu        sync.Mutex
	bufferedAmount atomic.Int64
}

// --- Subscriber interface (events.Subscriber) ---

// ID returns the connection identifier.
func (c *WsClient) ID() string { return c.connID }

// IsAuthenticated returns true if the client has completed the handshake.
func (c *WsClient) IsAuthenticated() bool { return c.authed }

// Role returns the client's RBAC role.
func (c *WsClient) Role() string {
	if c.role == "" {
		return "operator"
	}
	return c.role
}

// Scopes returns the client's permission scopes as strings.
func (c *WsClient) Scopes() []string {
	result := make([]string, len(c.scopes))
	for i, s := range c.scopes {
		result[i] = string(s)
	}
	return result
}

// SendEvent writes event data to the WebSocket.
func (c *WsClient) SendEvent(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.conn.Write(ctx, websocket.MessageText, data)
	if err == nil {
		c.bufferedAmount.Add(int64(len(data)))
	}
	return err
}

// BufferedAmount returns an estimate of queued bytes.
func (c *WsClient) BufferedAmount() int64 { return c.bufferedAmount.Load() }

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
		connID:  generateConnID(),
		created: time.Now(),
	}

	s.clients.Store(client.connID, client)
	s.clientCnt.Add(1)
	defer func() {
		s.broadcaster.Unsubscribe(client.connID)
		s.clients.Delete(client.connID)
		s.clientCnt.Add(-1)
	}()

	s.logger.Info("websocket connected", "connId", client.connID, "remote", r.RemoteAddr)

	// Handshake: first message must be a connect request.
	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), time.Duration(protocol.HandshakeTimeoutMs)*time.Millisecond)
	if err := s.handleHandshake(handshakeCtx, client, r.RemoteAddr); err != nil {
		handshakeCancel()
		s.logger.Warn("handshake failed", "connId", client.connID, "error", err)
		conn.Close(websocket.StatusPolicyViolation, "handshake failed")
		return
	}
	handshakeCancel()

	conn.SetReadLimit(protocol.MaxPayloadBytes)

	// Enter message loop (blocks until disconnect).
	// Ticks are handled by a shared server-level ticker (see startTickBroadcaster).
	s.runMessageLoop(r.Context(), client)

	s.logger.Info("websocket disconnected", "connId", client.connID)
}

// handleHandshake reads the first frame and validates it as a connect request.
func (s *Server) handleHandshake(ctx context.Context, client *WsClient, remoteAddr string) error {
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

	// Extract IP for rate limiting (strip port from remoteAddr).
	rateLimitKey := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		rateLimitKey = host
	}

	// Rate limit check before token validation.
	if s.authRateLimiter != nil && params.Auth != nil && params.Auth.Token != "" {
		allowed, retryMs := s.authRateLimiter.Check(rateLimitKey)
		if !allowed {
			errShape := protocol.NewError(protocol.ErrUnauthorized,
				fmt.Sprintf("rate limited, retry after %dms", retryMs))
			if writeErr := s.writeFrame(ctx, client, protocol.NewResponseError(req.ID, errShape)); writeErr != nil {
				s.logger.Error("failed to send rate limit error", "connId", client.connID, "error", writeErr)
			}
			return fmt.Errorf("auth rate limited")
		}
	}

	// Authenticate: validate token if auth validator is configured.
	if s.authValidator != nil && params.Auth != nil && params.Auth.Token != "" {
		claims, err := s.authValidator.ValidateToken(params.Auth.Token)
		if err != nil {
			// Record auth failure for rate limiting.
			if s.authRateLimiter != nil {
				s.authRateLimiter.RecordFailure(rateLimitKey)
			}
			errShape := protocol.NewError(protocol.ErrUnauthorized, "invalid token: "+err.Error())
			if writeErr := s.writeFrame(ctx, client, protocol.NewResponseError(req.ID, errShape)); writeErr != nil {
				s.logger.Error("failed to send auth error", "connId", client.connID, "error", writeErr)
			}
			return fmt.Errorf("token validation failed: %w", err)
		}
		// Reset rate limiter on successful auth.
		if s.authRateLimiter != nil {
			s.authRateLimiter.Reset(rateLimitKey)
		}
		client.authed = true
		client.role = string(claims.Role)
		client.deviceID = claims.DeviceID
		client.scopes = claims.Scopes
		s.authValidator.TouchDevice(claims.DeviceID)
	} else if s.authValidator == nil {
		// No-auth mode: trust all connections as operator.
		client.authed = true
		client.role = "operator"
		client.scopes = auth.DefaultScopes(auth.RoleOperator)
	} else {
		// Auth configured but no token provided: allow connection with limited access.
		client.authed = false
		client.role = params.Role
		if client.role == "" {
			client.role = "viewer"
		}
		client.scopes = auth.DefaultScopes(auth.Role(client.role))
	}

	// Register with broadcaster.
	s.broadcaster.Subscribe(client, events.Filter{})

	return s.writeFrame(ctx, client, helloResp)
}

func (s *Server) buildHelloOk(client *WsClient) *protocol.HelloOk {
	return &protocol.HelloOk{
		Type:     "hello-ok",
		Protocol: protocol.ProtocolVersion,
		Server:   protocol.HelloServer{Version: s.version, ConnID: client.connID},
		Features: protocol.HelloFeatures{
			Methods: s.dispatcher.Methods(),
			Events:  []string{"tick", "agent.event", "shutdown", "chat", "chat.delta"},
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
// Uses single-pass unmarshal: tries RequestFrame first (the common case),
// falling back to type-peek only for non-request frames.
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

		// Track activity on every inbound WS message.
		if s.activity != nil {
			s.activity.Touch()
		}

		// Single-pass: unmarshal directly as RequestFrame (dominant case).
		var req protocol.RequestFrame
		if err := json.Unmarshal(data, &req); err != nil {
			s.logger.Warn("unmarshal frame", "connId", client.connID, "error", err)
			// Send error response so the client knows the frame was rejected.
			errResp := protocol.NewResponseError("", protocol.NewError(
				protocol.ErrInvalidRequest, "malformed JSON frame"))
			if writeErr := s.writeFrame(ctx, client, errResp); writeErr != nil {
				return
			}
			continue
		}

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
			continue
		}

		// Dispatch with a per-request timeout to prevent stuck handlers.
		dispatchCtx, dispatchCancel := context.WithTimeout(ctx, dispatchTimeout)
		resp := s.dispatcher.Dispatch(dispatchCtx, &req)
		dispatchCancel()

		if err := s.writeFrame(ctx, client, resp); err != nil {
			s.logger.Warn("response write failed", "connId", client.connID, "method", req.Method, "error", err)
			return
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

// writeFrame serializes v as JSON and writes it to the WebSocket connection.
// Uses a pooled buffer to reduce GC pressure.
// The write is bounded by a 5-second timeout derived from the parent context.
func (s *Server) writeFrame(ctx context.Context, client *WsClient, v any) error {
	if client.conn == nil {
		return fmt.Errorf("connection closed")
	}

	buf := jsonBufPool.Get().(*bytes.Buffer)
	buf.Reset()

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		buf.Reset()
		jsonBufPool.Put(buf)
		s.logger.Error("marshal frame", "connId", client.connID, "error", err)
		return err
	}

	// json.Encoder.Encode appends a trailing newline; trim it for WS frames.
	data := buf.Bytes()
	if n := len(data); n > 0 && data[n-1] == '\n' {
		data = data[:n-1]
	}

	err := s.writeFrameRaw(ctx, client, data)
	buf.Reset()
	jsonBufPool.Put(buf)
	return err
}

// writeFrameRaw writes pre-serialized JSON bytes to the WebSocket.
func (s *Server) writeFrameRaw(ctx context.Context, client *WsClient, data []byte) error {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return client.conn.Write(writeCtx, websocket.MessageText, data)
}

// generateConnID returns a cryptographically random connection identifier
// to avoid collisions under concurrent connection bursts.
func generateConnID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails (should never happen).
		return fmt.Sprintf("conn-%d", time.Now().UnixNano())
	}
	return "conn-" + hex.EncodeToString(b)
}
