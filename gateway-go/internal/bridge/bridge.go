// Package bridge manages the Node.js plugin host subprocess.
//
// The Go gateway delegates plugin/extension execution to a Node.js process
// that hosts the TypeScript plugin SDK. Communication uses Unix domain sockets
// with the existing frame-based protocol (RequestFrame/ResponseFrame).
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PluginHost manages IPC communication with a Node.js plugin host process
// over a Unix domain socket. It implements the rpc.Forwarder interface.
type PluginHost struct {
	socketPath string
	conn       net.Conn
	writer     *FrameWriter
	reader     *FrameReader
	mu         sync.Mutex
	pending    map[string]chan *protocol.ResponseFrame
	running    bool
	logger     *slog.Logger
	closed     chan struct{}
	closeOnce  sync.Once
}

// New creates a new PluginHost (not yet started).
func New() *PluginHost {
	return &PluginHost{
		pending: make(map[string]chan *protocol.ResponseFrame),
		logger:  slog.Default(),
		closed:  make(chan struct{}),
	}
}

// NewWithSocket creates a PluginHost configured to connect to a Unix socket.
func NewWithSocket(socketPath string, logger *slog.Logger) *PluginHost {
	return &PluginHost{
		socketPath: socketPath,
		pending:    make(map[string]chan *protocol.ResponseFrame),
		logger:     logger,
		closed:     make(chan struct{}),
	}
}

// IsRunning reports whether the plugin host subprocess is active.
func (h *PluginHost) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// Connect establishes the Unix domain socket connection to the Plugin Host.
func (h *PluginHost) Connect(ctx context.Context) error {
	if h.socketPath == "" {
		return fmt.Errorf("no socket path configured")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", h.socketPath)
	if err != nil {
		return fmt.Errorf("connect to plugin host at %s: %w", h.socketPath, err)
	}
	h.mu.Lock()
	h.conn = conn
	h.writer = NewFrameWriter(conn)
	h.reader = NewFrameReader(conn)
	h.running = true
	h.mu.Unlock()

	go h.readLoop()
	return nil
}

// Forward sends an RPC request to the Plugin Host and waits for the response.
// This implements the rpc.Forwarder interface.
func (h *PluginHost) Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	respCh := make(chan *protocol.ResponseFrame, 1)

	h.mu.Lock()
	h.pending[req.ID] = respCh
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, req.ID)
		h.mu.Unlock()
	}()

	h.mu.Lock()
	err := h.writer.WriteRequest(req)
	h.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.closed:
		return nil, fmt.Errorf("bridge connection closed")
	}
}

// Close closes the bridge connection. Safe to call multiple times.
func (h *PluginHost) Close() error {
	var closeErr error
	h.closeOnce.Do(func() {
		close(h.closed)
		h.mu.Lock()
		h.running = false
		conn := h.conn
		h.mu.Unlock()
		if conn != nil {
			closeErr = conn.Close()
		}
	})
	return closeErr
}

// readLoop reads frames from the Plugin Host and dispatches responses.
func (h *PluginHost) readLoop() {
	defer func() {
		// Use closeOnce to safely close the channel (may race with Close()).
		h.closeOnce.Do(func() {
			close(h.closed)
		})
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()
	}()

	for {
		frameType, data, err := h.reader.ReadFrame()
		if err != nil {
			select {
			case <-h.closed:
				return
			default:
				h.logger.Error("bridge read error", "error", err)
				return
			}
		}

		switch frameType {
		case protocol.FrameTypeResponse:
			var resp protocol.ResponseFrame
			if err := json.Unmarshal(data, &resp); err != nil {
				h.logger.Error("unmarshal response", "error", err)
				continue
			}
			h.mu.Lock()
			ch, ok := h.pending[resp.ID]
			h.mu.Unlock()
			if ok {
				// Non-blocking send: if Forward() already returned (timeout/cancel),
				// the buffered channel absorbs the value without blocking.
				select {
				case ch <- &resp:
				default:
				}
			}

		case protocol.FrameTypeEvent:
			h.logger.Debug("bridge event received", "data", string(data))

		default:
			h.logger.Warn("unexpected frame type from bridge", "type", string(frameType))
		}
	}
}
