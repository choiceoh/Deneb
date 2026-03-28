package propus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"nhooyr.io/websocket"
)

// ChatSendFunc is the callback to dispatch a message through Deneb's chat pipeline.
// sessionKey identifies the Propus session; message is the user text.
// The function should trigger an async agent run; streaming events arrive via OnEvent.
type ChatSendFunc func(sessionKey, message string)

// SessionClearFunc clears a session's conversation history.
type SessionClearFunc func(sessionKey string)

// SessionAbortFunc aborts all active runs for a session key.
type SessionAbortFunc func(sessionKey string)

// Plugin implements channel.Plugin for the Propus desktop coding client.
type Plugin struct {
	config *Config
	logger *slog.Logger

	mu     sync.Mutex
	status channel.Status
	server *http.Server
	ln     net.Listener

	// Callbacks wired by the gateway after construction.
	chatSend     ChatSendFunc
	sessionClear SessionClearFunc
	sessionAbort SessionAbortFunc

	// Active client connections (connID → conn).
	clients   map[string]*clientConn
	clientsMu sync.RWMutex
}

// clientConn tracks a single Propus WebSocket client.
type clientConn struct {
	conn   *websocket.Conn
	connID string
	mu     sync.Mutex // serializes writes
}

// NewPlugin creates a new Propus channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config:  cfg,
		logger:  logger,
		status:  channel.Status{Connected: false},
		clients: make(map[string]*clientConn),
	}
}

// SetChatSend wires the chat dispatch callback.
func (p *Plugin) SetChatSend(fn ChatSendFunc) { p.chatSend = fn }

// SetSessionClear wires the session clear callback.
func (p *Plugin) SetSessionClear(fn SessionClearFunc) { p.sessionClear = fn }

// SetSessionAbort wires the session abort callback.
func (p *Plugin) SetSessionAbort(fn SessionAbortFunc) { p.sessionAbort = fn }

// --- channel.Plugin interface ---

func (p *Plugin) ID() string { return "propus" }

func (p *Plugin) Meta() channel.Meta {
	return channel.Meta{
		ID:    "propus",
		Label: "Propus",
		Blurb: "Desktop coding assistant channel",
	}
}

func (p *Plugin) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		ChatTypes:      []string{"coding"},
		BlockStreaming: false,
	}
}

func (p *Plugin) Start(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.config.Enabled {
		p.status = channel.Status{Connected: false, Error: "disabled"}
		return nil
	}

	addr := p.config.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		p.status = channel.Status{Connected: false, Error: "listen: " + err.Error()}
		return fmt.Errorf("propus: listen %s: %w", addr, err)
	}
	p.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", p.handleWS)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	p.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		p.logger.Info("propus channel listening", "addr", addr)
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			p.logger.Error("propus serve error", "error", err)
		}
	}()

	p.status = channel.Status{Connected: true}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close all active WebSocket connections so read loops exit.
	p.clientsMu.Lock()
	for id, cc := range p.clients {
		cc.conn.Close(websocket.StatusGoingAway, "server shutting down")
		delete(p.clients, id)
	}
	p.clientsMu.Unlock()

	if p.server != nil {
		_ = p.server.Shutdown(ctx)
	}
	p.status = channel.Status{Connected: false}
	return nil
}

func (p *Plugin) Status() channel.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// --- WebSocket handler ---

func (p *Plugin) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // local only, no CORS needed
	})
	if err != nil {
		p.logger.Error("propus ws accept error", "error", err)
		return
	}

	connID := fmt.Sprintf("propus-%d", time.Now().UnixNano())
	cc := &clientConn{conn: conn, connID: connID}

	p.clientsMu.Lock()
	p.clients[connID] = cc
	p.clientsMu.Unlock()

	p.logger.Info("propus client connected", "connID", connID)

	// Send initial config status.
	p.sendToClient(cc, MsgConfigStatus("deneb", "Deneb Gateway", "connected"))

	defer func() {
		p.clientsMu.Lock()
		delete(p.clients, connID)
		p.clientsMu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "bye")
		p.logger.Info("propus client disconnected", "connID", connID)
	}()

	// Read loop.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				return // normal close
			}
			p.logger.Warn("propus ws read error", "error", err, "connID", connID)
			return
		}
		p.handleMessage(cc, data)
	}
}

func (p *Plugin) handleMessage(cc *clientConn, data []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		p.sendToClient(cc, MsgError("invalid message: "+err.Error()))
		return
	}

	switch msg.Type {
	case "SendMessage":
		var d SendMessageData
		if err := json.Unmarshal(msg.Data, &d); err != nil {
			p.sendToClient(cc, MsgError("invalid SendMessage data"))
			return
		}
		if d.Text == "" {
			return
		}
		sessionKey := "propus:" + cc.connID
		if p.chatSend != nil {
			p.chatSend(sessionKey, d.Text)
		} else {
			p.sendToClient(cc, MsgError("chat handler not configured"))
		}

	case "ClearChat":
		sessionKey := "propus:" + cc.connID
		if p.sessionAbort != nil {
			p.sessionAbort(sessionKey)
		}
		if p.sessionClear != nil {
			p.sessionClear(sessionKey)
		}
		p.sendToClient(cc, MsgChatCleared())

	case "Ping":
		p.sendToClient(cc, MsgPong())

	case "StopGeneration":
		sessionKey := "propus:" + cc.connID
		if p.sessionAbort != nil {
			p.sessionAbort(sessionKey)
		}
		p.logger.Info("propus generation stopped", "connID", cc.connID)

	case "SaveSession":
		p.sendToClient(cc, MsgError("session save not yet supported"))

	case "SetApiKey":
		// API key is shared with Deneb — no separate key needed.
		p.sendToClient(cc, MsgConfigStatus("deneb", "Deneb Gateway", "connected"))

	default:
		p.logger.Warn("propus unknown message type", "type", msg.Type)
	}
}

// --- Outbound: Deneb events → Propus client ---

// BroadcastToAll sends a ServerMessage to all connected Propus clients.
func (p *Plugin) BroadcastToAll(msg ServerMessage) {
	p.clientsMu.RLock()
	defer p.clientsMu.RUnlock()
	for _, cc := range p.clients {
		p.sendToClient(cc, msg)
	}
}

// BroadcastToSession sends a ServerMessage to the client owning the given session key.
func (p *Plugin) BroadcastToSession(sessionKey string, msg ServerMessage) {
	connID := strings.TrimPrefix(sessionKey, "propus:")
	p.clientsMu.RLock()
	cc, ok := p.clients[connID]
	p.clientsMu.RUnlock()
	if ok {
		p.sendToClient(cc, msg)
	}
}

func (p *Plugin) sendToClient(cc *clientConn, msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cc.conn.Write(ctx, websocket.MessageText, data); err != nil {
		p.logger.Warn("propus ws write error", "error", err, "connID", cc.connID)
	}
}

// Compile-time interface check.
var _ channel.Plugin = (*Plugin)(nil)
