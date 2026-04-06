package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"nhooyr.io/websocket"
)

const (
	// maxWebSocketClients limits the number of concurrent WebSocket connections.
	maxWebSocketClients = 256
)

// jsonBufPool reduces GC pressure for writeFrame by reusing marshal buffers.
var jsonBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

// isRoutineConnError returns true for errors expected from clients that connect
// but never complete the handshake (health probes, port scans, CLI retries).
func isRoutineConnError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	// net/internal poll wraps "use of closed network connection" without a
	// sentinel — fall back to string match.
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection")
}

// Connection health constants.
const (
	// pingInterval is how often the server sends WebSocket pings to detect dead connections.
	pingInterval = 30 * time.Second
	// pingTimeout is how long to wait for a pong response before counting a failure.
	pingTimeout = 20 * time.Second
	// maxPingFailures is how many consecutive ping failures are tolerated before closing.
	// Single DGX Spark: GPU inference can block pong responses temporarily.
	maxPingFailures = 3
	// idleTimeout disconnects clients that haven't sent any messages.
	idleTimeout = 30 * time.Minute
	// maxConsecutiveBadFrames closes connections that send too many malformed frames.
	maxConsecutiveBadFrames = 3
)

// WsClient represents a connected WebSocket client.
// Implements events.Subscriber for event broadcasting.
type WsClient struct {
	conn          *websocket.Conn
	connID        string
	created       time.Time
	role          string
	authed        bool
	deviceID      string
	writeMu       sync.Mutex
	inflightBytes atomic.Int64 // bytes currently being written (not yet flushed)
	lastActivity  atomic.Int64 // unix nano of last inbound message
	cancelPing    context.CancelFunc
}

// --- Subscriber interface (events.Subscriber) ---

// ID returns the connection identifier.
func (c *WsClient) ID() string { return c.connID }

// IsAuthenticated returns true if the client has completed the handshake.
func (c *WsClient) IsAuthenticated() bool { return c.authed }

// eventWriteTimeout is the deadline for writing a single event frame.
// Generous to handle bursts during LLM streaming on loaded DGX Spark.
const eventWriteTimeout = 15 * time.Second

// SendEvent writes event data to the WebSocket.
// inflightBytes tracks bytes during write for slow consumer detection.
func (c *WsClient) SendEvent(data []byte) error {
	n := int64(len(data))
	c.inflightBytes.Add(n)
	defer c.inflightBytes.Add(-n)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), eventWriteTimeout)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// BufferedAmount returns bytes currently in-flight (being written).
// Used by the broadcaster for slow consumer detection.
func (c *WsClient) BufferedAmount() int64 { return c.inflightBytes.Load() }

// touchActivity records the current time as last inbound activity.
func (c *WsClient) touchActivity() { c.lastActivity.Store(time.Now().UnixNano()) }

// idleDuration returns how long since the last inbound message.
func (c *WsClient) idleDuration() time.Duration {
	last := c.lastActivity.Load()
	if last == 0 {
		return time.Since(c.created)
	}
	return time.Since(time.Unix(0, last))
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
		connID:  generateConnID(),
		created: time.Now(),
	}
	client.touchActivity()

	s.clients.Store(client.connID, client)
	s.clientCnt.Add(1)
	defer func() {
		if client.cancelPing != nil {
			client.cancelPing()
		}
		s.broadcaster.Unsubscribe(client.connID)
		s.clients.Delete(client.connID)
		s.clientCnt.Add(-1)
	}()

	// Handshake: first message must be a connect request.
	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), time.Duration(protocol.HandshakeTimeoutMs)*time.Millisecond)
	if err := s.handleHandshake(handshakeCtx, client, r.RemoteAddr); err != nil {
		handshakeCancel()
		// Timeout / closed-conn are routine (health checks, port scans, CLI retries) — silent drop.
		if !isRoutineConnError(err) {
			s.logger.Warn("handshake failed", "connId", client.connID, "error", err)
		}
		conn.Close(websocket.StatusPolicyViolation, "handshake failed")
		return
	}
	handshakeCancel()

	s.logger.Info("websocket connected", "connId", client.connID, "remote", r.RemoteAddr)

	conn.SetReadLimit(protocol.MaxPayloadBytes)

	// Start ping/pong + idle timeout goroutine for connection health monitoring.
	pingCtx, pingCancel := context.WithCancel(r.Context())
	client.cancelPing = pingCancel
	go s.runPingLoop(pingCtx, client)

	// Enter message loop (blocks until disconnect).
	// Ticks are handled by a shared server-level ticker (see startTickBroadcaster).
	s.runMessageLoop(r.Context(), client)

	s.logger.Info("websocket disconnected", "connId", client.connID)
}

// handleHandshake sends a connect.challenge event, then reads and validates the connect request.
func (s *Server) handleHandshake(ctx context.Context, client *WsClient, remoteAddr string) error {
	// Send connect.challenge event with random nonce before reading connect frame.
	// This prevents replay attacks and proves device identity.
	// Mirrors src/gateway/server/ws-connection.ts.
	challengeNonce := generateChallengeNonce()
	challengeEvent, _ := json.Marshal(map[string]any{
		"type":  "event",
		"event": "connect.challenge",
		"payload": map[string]any{
			"nonce": challengeNonce,
			"ts":    time.Now().UnixMilli(),
		},
	})
	challengeCtx, challengeCancel := context.WithTimeout(ctx, 5*time.Second)
	err := client.conn.Write(challengeCtx, 1 /* TextMessage */, challengeEvent)
	challengeCancel()
	if err != nil {
		return fmt.Errorf("send connect.challenge: %w", err)
	}

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
		rpcErr := rpcerr.Newf(protocol.ErrInvalidRequest, "protocol version mismatch: server=%d, client range=%d-%d",
			protocol.ProtocolVersion, params.MinProtocol, params.MaxProtocol)
		if err := s.writeFrame(ctx, client, rpcErr.Response(req.ID)); err != nil {
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
			rpcErr := rpcerr.Newf(protocol.ErrUnauthorized, "rate limited, retry after %dms", retryMs)
			if writeErr := s.writeFrame(ctx, client, rpcErr.Response(req.ID)); writeErr != nil {
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
			rpcErr := rpcerr.Unauthorized("invalid token: " + err.Error())
			if writeErr := s.writeFrame(ctx, client, rpcErr.Response(req.ID)); writeErr != nil {
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
		s.authValidator.TouchDevice(claims.DeviceID)
	} else if s.authValidator == nil {
		// No-auth mode: trust all connections as operator.
		client.authed = true
		client.role = "operator"
	} else {
		// Auth configured but no token provided: limited access (probe role).
		client.authed = false
		client.role = params.Role
		if client.role == "" {
			client.role = "probe"
		}
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

// generateChallengeNonce returns a 16-byte hex-encoded random nonce for the
// connect.challenge WebSocket event (prevents replay attacks).
func generateChallengeNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("nonce-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
