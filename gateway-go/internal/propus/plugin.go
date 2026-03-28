package propus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

// SessionSaveFunc exports a session transcript to disk and returns the file path.
type SessionSaveFunc func(sessionKey string) (string, error)

// MediaSendFunc delivers a file to the Propus client for a given session.
type MediaSendFunc func(sessionKey, filePath, mediaType, caption string) error

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
	sessionSave  SessionSaveFunc

	// Model info for ConfigStatus messages.
	modelName   string
	serviceName string

	// Active client connections (connID → conn).
	clients   map[string]*clientConn
	clientsMu sync.RWMutex

	// Temporary file store for download serving (fileID → fileEntry).
	files   map[string]*fileEntry
	filesMu sync.RWMutex
}

// clientConn tracks a single Propus WebSocket client.
type clientConn struct {
	conn     *websocket.Conn
	connID   string
	mu       sync.Mutex // serializes writes
	lastPong time.Time  // last time a Pong was received
}

// fileEntry is a temporary file registered for HTTP download.
type fileEntry struct {
	path      string
	expiresAt time.Time
}

// NewPlugin creates a new Propus channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config:  cfg,
		logger:  logger,
		status:  channel.Status{Connected: false},
		clients: make(map[string]*clientConn),
		files:   make(map[string]*fileEntry),
	}
}

// SetChatSend wires the chat dispatch callback.
func (p *Plugin) SetChatSend(fn ChatSendFunc) { p.chatSend = fn }

// SetSessionClear wires the session clear callback.
func (p *Plugin) SetSessionClear(fn SessionClearFunc) { p.sessionClear = fn }

// SetSessionAbort wires the session abort callback.
func (p *Plugin) SetSessionAbort(fn SessionAbortFunc) { p.sessionAbort = fn }

// SetSessionSave wires the session save/export callback.
func (p *Plugin) SetSessionSave(fn SessionSaveFunc) { p.sessionSave = fn }

// SetModelInfo stores the model/service names used in ConfigStatus messages.
func (p *Plugin) SetModelInfo(model, service string) {
	p.modelName = model
	p.serviceName = service
}

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
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/files/", p.handleFileDownload)

	p.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		p.logger.Info("propus channel listening", "addr", addr)
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			p.logger.Error("propus serve error", "error", err)
		}
	}()

	// Start background file cleanup goroutine.
	go p.fileCleanupLoop()

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

// --- Health endpoint ---

func (p *Plugin) handleHealth(w http.ResponseWriter, _ *http.Request) {
	p.clientsMu.RLock()
	clientCount := len(p.clients)
	p.clientsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := fmt.Sprintf(`{"status":"ok","clients":%d}`, clientCount)
	_, _ = w.Write([]byte(resp))
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

	// Support session resume: if client passes ?resume=<connID>, reuse that ID
	// so the server-side session key (propus:<connID>) is preserved.
	resumeID := r.URL.Query().Get("resume")
	var connID string
	if resumeID != "" {
		connID = resumeID
		p.logger.Info("propus client resuming session", "connID", connID)
	} else {
		connID = fmt.Sprintf("propus-%d", time.Now().UnixNano())
		p.logger.Info("propus client connected", "connID", connID)
	}
	cc := &clientConn{conn: conn, connID: connID, lastPong: time.Now()}

	// If resuming, close the old connection for this connID (if any).
	p.clientsMu.Lock()
	if old, ok := p.clients[connID]; ok && old.conn != conn {
		old.conn.Close(websocket.StatusGoingAway, "replaced by resume")
	}
	p.clients[connID] = cc
	p.clientsMu.Unlock()

	// Send initial config status with conn_id so client can resume later.
	p.sendToClient(cc, p.configStatusMsg("connected", connID))

	// Start server-side heartbeat for this connection.
	heartCtx, heartCancel := context.WithCancel(r.Context())

	defer func() {
		heartCancel()
		p.clientsMu.Lock()
		delete(p.clients, connID)
		p.clientsMu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "bye")
		p.logger.Info("propus client disconnected", "connID", connID)
	}()

	go p.heartbeatLoop(heartCtx, cc)

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

// heartbeatLoop sends periodic Ping messages and disconnects stale clients.
func (p *Plugin) heartbeatLoop(ctx context.Context, cc *clientConn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cc.mu.Lock()
			stale := time.Since(cc.lastPong) > 90*time.Second
			cc.mu.Unlock()

			if stale {
				p.logger.Warn("propus client heartbeat timeout, closing", "connID", cc.connID)
				cc.conn.Close(websocket.StatusGoingAway, "heartbeat timeout")
				return
			}

			// Send a Ping message (application-level).
			p.sendToClient(cc, MsgPong()) // client responds with Ping, server sends Pong
		}
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

	case "SaveSession":
		sessionKey := "propus:" + cc.connID
		if p.sessionSave != nil {
			path, err := p.sessionSave(sessionKey)
			if err != nil {
				p.sendToClient(cc, MsgError("세션 저장 실패: "+err.Error()))
			} else {
				p.sendToClient(cc, MsgSessionSaved(path))
			}
		} else {
			p.sendToClient(cc, MsgError("session save not configured"))
		}

	case "Ping":
		// Update heartbeat timestamp on any client activity.
		cc.mu.Lock()
		cc.lastPong = time.Now()
		cc.mu.Unlock()
		p.sendToClient(cc, MsgPong())

	case "StopGeneration":
		sessionKey := "propus:" + cc.connID
		if p.sessionAbort != nil {
			p.sessionAbort(sessionKey)
		}
		p.logger.Info("propus generation stopped", "connID", cc.connID)

	case "SetApiKey":
		// API key is shared with Deneb — no separate key needed.
		p.sendToClient(cc, p.configStatusMsg("connected", cc.connID))

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
		// Retry once after a short delay.
		time.Sleep(500 * time.Millisecond)
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer retryCancel()
		if retryErr := cc.conn.Write(retryCtx, websocket.MessageText, data); retryErr != nil {
			p.logger.Warn("propus ws write failed after retry",
				"error", retryErr, "msgType", msg.Type, "connID", cc.connID)
			// Mark connection as stale; heartbeat loop will clean up.
		}
	}
}

// --- File serving for media delivery ---

// RegisterFile stores a file for temporary HTTP download and returns the file ID.
func (p *Plugin) RegisterFile(filePath string) string {
	id := randomFileID()
	p.filesMu.Lock()
	p.files[id] = &fileEntry{
		path:      filePath,
		expiresAt: time.Now().Add(1 * time.Hour),
	}
	p.filesMu.Unlock()
	return id
}

// FileDownloadURL returns the full HTTP URL for a registered file.
func (p *Plugin) FileDownloadURL(fileID string) string {
	return fmt.Sprintf("http://%s/files/%s", p.config.ListenAddr(), fileID)
}

func (p *Plugin) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/files/")
	if fileID == "" {
		http.NotFound(w, r)
		return
	}

	p.filesMu.RLock()
	entry, ok := p.files[fileID]
	p.filesMu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, entry.path)
}

// fileCleanupLoop removes expired file entries every 10 minutes.
func (p *Plugin) fileCleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		p.filesMu.Lock()
		for id, entry := range p.files {
			if now.After(entry.expiresAt) {
				delete(p.files, id)
			}
		}
		p.filesMu.Unlock()
	}
}

func randomFileID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Compile-time interface check.
var _ channel.Plugin = (*Plugin)(nil)

// configStatusMsg builds a ConfigStatus message with stored model info.
func (p *Plugin) configStatusMsg(denebStatus, connID string) ServerMessage {
	model := p.modelName
	if model == "" {
		model = "deneb"
	}
	svc := p.serviceName
	if svc == "" {
		svc = "Deneb Gateway"
	}
	return MsgConfigStatus(model, svc, denebStatus, connID)
}

// ToolProfile returns the configured tool profile (e.g. "coding").
func (p *Plugin) ToolProfile() string {
	return p.config.Tools
}

// ListenAddr exposes the configured listen address for use by server wiring.
func (p *Plugin) ListenAddr() string {
	return p.config.ListenAddr()
}

// --- Exported helpers for file delivery via media send ---

// SendFileToSession registers a file for download and notifies the session's client.
func (p *Plugin) SendFileToSession(sessionKey, filePath, mediaType, caption string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	fileID := p.RegisterFile(filePath)
	url := p.FileDownloadURL(fileID)
	name := filepath.Base(filePath)

	p.BroadcastToSession(sessionKey, MsgFile(name, mediaType, info.Size(), url))

	if caption != "" {
		p.BroadcastToSession(sessionKey, MsgText("📎 "+caption))
	}
	return nil
}
