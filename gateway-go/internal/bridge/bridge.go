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
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// EventHandler is called when the bridge receives an event frame from Node.js.
// Implementations must not block; long-running work should be dispatched to a goroutine.
type EventHandler func(event *protocol.EventFrame)

// PluginHost manages IPC communication with a Node.js plugin host process
// over a Unix domain socket. It implements the rpc.Forwarder interface.
type PluginHost struct {
	socketPath string
	conn       net.Conn
	writer     *FrameWriter
	reader     *FrameReader
	mu         sync.Mutex // protects pending map and running flag
	writeMu    sync.Mutex // serializes socket writes (separate from mu to avoid holding mu during I/O)
	pending    map[string]chan *protocol.ResponseFrame
	running    bool
	logger     *slog.Logger
	// closed is signaled when the current connection is done.
	// Protected by closeMu to prevent double-close.
	closed  chan struct{}
	closeMu sync.Mutex
	// reconnect controls automatic reconnection.
	reconnect     bool
	reconnectStop chan struct{}
	reconnectWg   sync.WaitGroup // tracks reconnectLoop goroutine
	// onEvent is called for each event frame received from the bridge.
	onEvent EventHandler
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
	return h.dial(ctx)
}

// ConnectWithReconnect connects and automatically reconnects on failure
// with exponential backoff (1s, 2s, 4s, ..., max 30s).
func (h *PluginHost) ConnectWithReconnect(ctx context.Context) error {
	if err := h.dial(ctx); err != nil {
		return err
	}
	h.reconnect = true
	h.reconnectStop = make(chan struct{})
	return nil
}

func (h *PluginHost) dial(ctx context.Context) error {
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

const (
	// defaultForwardTimeout is the maximum time to wait for a bridge response
	// if the caller's context has no deadline.
	defaultForwardTimeout = 60 * time.Second
)

// Forward sends an RPC request to the Plugin Host and waits for the response.
// If the caller's context has no deadline, a default 60-second timeout is applied.
// This implements the rpc.Forwarder interface.
func (h *PluginHost) Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	if !h.IsRunning() {
		return nil, fmt.Errorf("bridge not connected")
	}

	// Apply default timeout if no deadline is set.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultForwardTimeout)
		defer cancel()
	}

	respCh := make(chan *protocol.ResponseFrame, 1)

	// Register pending entry under mu (fast path, no I/O).
	h.mu.Lock()
	h.pending[req.ID] = respCh
	h.mu.Unlock()

	// Clean up pending entry on all exit paths.
	defer func() {
		h.mu.Lock()
		delete(h.pending, req.ID)
		h.mu.Unlock()
	}()

	// Write under writeMu (separate from mu to avoid blocking pending map
	// lookups in readLoop while I/O is in progress).
	h.writeMu.Lock()
	err := h.writer.WriteRequest(req)
	h.writeMu.Unlock()
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

// SetEventHandler registers a callback for event frames from the Node.js plugin host.
// Must be called before Connect. Not safe to call concurrently with readLoop.
func (h *PluginHost) SetEventHandler(handler EventHandler) {
	h.onEvent = handler
}

// Close closes the bridge connection and waits for background goroutines.
// Safe to call multiple times.
func (h *PluginHost) Close() error {
	// Stop reconnect loop if running.
	if h.reconnectStop != nil {
		select {
		case <-h.reconnectStop:
		default:
			close(h.reconnectStop)
		}
	}

	h.signalClosed()

	h.mu.Lock()
	h.running = false
	conn := h.conn
	h.mu.Unlock()

	// Wait for reconnect goroutine to finish (prevents goroutine leak).
	h.reconnectWg.Wait()

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// signalClosed safely closes the h.closed channel exactly once.
func (h *PluginHost) signalClosed() {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	select {
	case <-h.closed:
		// already closed
	default:
		close(h.closed)
	}
}

// readLoop reads frames from the Plugin Host and dispatches responses.
func (h *PluginHost) readLoop() {
	defer func() {
		h.signalClosed()
		h.mu.Lock()
		h.running = false
		h.mu.Unlock()

		// Attempt reconnect if enabled.
		if h.reconnect {
			h.reconnectWg.Add(1)
			go func() {
				defer h.reconnectWg.Done()
				h.reconnectLoop()
			}()
		}
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
				select {
				case ch <- &resp:
				default:
					h.logger.Warn("bridge response dropped: caller already timed out", "id", resp.ID)
				}
			}

		case protocol.FrameTypeEvent:
			var ev protocol.EventFrame
			if err := json.Unmarshal(data, &ev); err != nil {
				h.logger.Error("unmarshal bridge event", "error", err)
				continue
			}
			if h.onEvent != nil {
				h.onEvent(&ev)
			} else {
				h.logger.Debug("bridge event received (no handler)", "event", ev.Event)
			}

		default:
			h.logger.Warn("unexpected frame type from bridge", "type", string(frameType))
		}
	}
}

// reconnectLoop attempts to re-establish the bridge connection with
// exponential backoff: 1s, 2s, 4s, ..., capped at 30s.
func (h *PluginHost) reconnectLoop() {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-h.reconnectStop:
			return
		case <-time.After(backoff):
		}

		h.logger.Info("bridge reconnecting", "socket", h.socketPath, "backoff", backoff)

		// Drain pending entries: send error responses so Forward() callers
		// unblock instead of waiting on a stale closed channel.
		h.mu.Lock()
		for id, ch := range h.pending {
			errResp := &protocol.ResponseFrame{
				Type:  protocol.FrameTypeResponse,
				ID:    id,
				OK:    false,
				Error: protocol.NewError(protocol.ErrUnavailable, "bridge connection lost, reconnecting"),
			}
			select {
			case ch <- errResp:
			default:
			}
			delete(h.pending, id)
		}
		h.mu.Unlock()

		// Reset closed channel for the new connection attempt.
		h.closeMu.Lock()
		h.closed = make(chan struct{})
		h.closeMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := h.dial(ctx)
		cancel()

		if err == nil {
			h.logger.Info("bridge reconnected", "socket", h.socketPath)
			return
		}

		h.logger.Warn("bridge reconnect failed", "error", err, "retryIn", backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
