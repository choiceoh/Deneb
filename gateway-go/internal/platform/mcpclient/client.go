// Package mcpclient implements a minimal client for the Model Context
// Protocol (MCP) stdio transport: it spawns a configured server command and
// speaks newline-delimited JSON-RPC 2.0 over the child's stdin/stdout.
//
// Scope is deliberately narrow (mirror of server_http_mcp.go's minimalism on
// the serving side): initialize handshake, tools/list, tools/call. No
// resources/prompts/sampling — server-initiated requests are answered with
// "method not found" so a well-behaved server degrades gracefully.
//
// The stdio transport (not streamable HTTP) is the deliberate choice for
// external servers like Plaud's: their npx wrapper owns the OAuth dance and
// token cache, which a headless gateway cannot do against a browser-redirect
// flow.
package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// protocolVersion is the newest MCP spec revision this client speaks.
const protocolVersion = "2025-06-18"

// restartBackoff is the minimum interval between respawn attempts after the
// child process dies — prevents a crash-looping server from being re-exec'd
// on every tool call.
const restartBackoff = 30 * time.Second

// maxLineBytes caps a single JSON-RPC message from the server (tool results
// can embed whole meeting transcripts; 16 MiB is generous without being
// unbounded).
const maxLineBytes = 16 << 20

// ToolInfo is one entry from tools/list.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Client manages one MCP server child process. Calls may run concurrently;
// a dead child is respawned lazily on the next call (restartBackoff-gated).
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	Client.readyMu (spawn + handshake serialization; held across handshake I/O)
//	  → Client.mu (process state + stdin write serialization)
//	      → Client.pendingMu (in-flight request table; reader goroutine takes it alone)
type Client struct {
	argv   []string
	logger *slog.Logger
	// lifeCtx bounds the child process's lifetime (exec.CommandContext):
	// typically the server shutdown ctx, so the child dies with the gateway.
	lifeCtx context.Context

	readyMu sync.Mutex

	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	ready      bool // process running AND initialize handshake completed
	lastStart  time.Time
	closed     bool
	generation int // bumped per spawn; a stale process-exit event no-ops

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse
	nextID    int64
}

type rpcResponse struct {
	Result json.RawMessage
	Err    error
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// New creates a client for the given command line (argv[0] = binary). The
// process is not spawned until Start or the first call. lifeCtx bounds the
// child process's lifetime (pass the server shutdown ctx); Close also kills it.
func New(lifeCtx context.Context, argv []string, logger *slog.Logger) (*Client, error) {
	if len(argv) == 0 {
		return nil, errors.New("mcpclient: empty command")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if lifeCtx == nil {
		lifeCtx = context.Background()
	}
	return &Client{
		argv:    argv,
		logger:  logger,
		lifeCtx: lifeCtx,
		pending: make(map[int64]chan rpcResponse),
	}, nil
}

// Start spawns the server process and performs the initialize handshake.
// ctx bounds the handshake only; the process itself lives until Close.
func (c *Client) Start(ctx context.Context) error {
	return c.ensureReady(ctx)
}

// ListTools returns the server's full tool catalog (follows pagination).
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if err := c.ensureReady(ctx); err != nil {
		return nil, err
	}
	var out []ToolInfo
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.roundTrip(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("mcpclient: tools/list result: %w", err)
		}
		out = append(out, page.Tools...)
		if page.NextCursor == "" || len(page.Tools) == 0 {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes a named tool and renders the result's content blocks as
// text. A result with isError=true is returned as a Go error so the agent
// executor surfaces it to the model as a tool failure.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if err := c.ensureReady(ctx); err != nil {
		return "", err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	raw, err := c.roundTrip(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("mcpclient: tools/call result: %w", err)
	}
	var sb strings.Builder
	for _, block := range res.Content {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		if block.Type == "text" {
			sb.WriteString(block.Text)
		} else {
			// Non-text blocks (image/audio/resource) — name them rather than
			// dropping silently so the model knows something was elided.
			fmt.Fprintf(&sb, "[%s content omitted]", block.Type)
		}
	}
	if res.IsError {
		return "", fmt.Errorf("mcp tool %s failed: %s", name, sb.String())
	}
	return sb.String(), nil
}

// Close terminates the server process. Safe to call more than once.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.stopLocked()
}

// ensureReady spawns the child and runs the initialize handshake if needed.
// readyMu serializes concurrent callers so the handshake happens exactly once
// per spawn; the handshake's request/response goes through the normal pending
// path (reader goroutine), holding neither mu nor pendingMu while waiting.
func (c *Client) ensureReady(ctx context.Context) error {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()

	c.mu.Lock()
	if c.ready {
		c.mu.Unlock()
		return nil
	}
	err := c.spawnLocked()
	gen := c.generation
	c.mu.Unlock()
	if err != nil {
		return err
	}

	if err := c.handshake(ctx); err != nil {
		c.mu.Lock()
		if c.generation == gen {
			c.stopLocked()
		}
		c.mu.Unlock()
		return err
	}

	c.mu.Lock()
	if c.generation == gen && c.cmd != nil {
		c.ready = true
	}
	c.mu.Unlock()
	return nil
}

// spawnLocked starts the child process. Caller holds c.mu.
func (c *Client) spawnLocked() error {
	if c.closed {
		return errors.New("mcpclient: client closed")
	}
	if c.cmd != nil {
		return nil // already running (handshake pending or done)
	}
	if !c.lastStart.IsZero() && time.Since(c.lastStart) < restartBackoff {
		return fmt.Errorf("mcpclient: server process down, respawn backoff active (%s)", restartBackoff)
	}
	c.lastStart = time.Now()

	cmd := exec.CommandContext(c.lifeCtx, c.argv[0], c.argv[1:]...) //nolint:gosec // G204 — argv is operator config (DENEB_*_MCP_CMD env), not request input.
	cmd.Env = os.Environ()                                          // npx needs PATH/HOME (OAuth token cache lives under $HOME)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcpclient: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcpclient: start %q: %w", c.argv[0], err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.generation++
	gen := c.generation
	c.logger.Info("mcp server process started", "cmd", c.argv[0], "pid", cmd.Process.Pid)

	// Reader goroutine: routes responses to pending calls. Exits on stdout
	// EOF (process death or Close), so it always has a termination path.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("panic in mcp reader goroutine", "panic", r)
			}
			c.onProcessExit(gen)
		}()
		c.readLoop(stdout)
	}()

	// Stderr goroutine: surfaces the server's own diagnostics — first-run
	// OAuth instructions from wrappers like Plaud's npx package arrive here,
	// so the operator must be able to see them in the gateway log. Exits on
	// stderr EOF alongside the process.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("panic in mcp stderr goroutine", "panic", r)
			}
		}()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 256*1024)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				c.logger.Info("mcp server stderr", "cmd", c.argv[0], "line", line)
			}
		}
	}()

	return nil
}

// handshake performs initialize + notifications/initialized.
func (c *Client) handshake(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "deneb-gateway", "version": "1.0"},
	}
	raw, err := c.roundTrip(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("mcpclient: initialize: %w", err)
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		return fmt.Errorf("mcpclient: initialize result: %w", err)
	}
	if err := c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return err
	}
	c.logger.Info("mcp server initialized",
		"server", init.ServerInfo.Name,
		"serverVersion", init.ServerInfo.Version,
		"protocolVersion", init.ProtocolVersion)
	return nil
}

// stopLocked kills the child and marks the client not ready. Caller holds c.mu.
func (c *Client) stopLocked() {
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		// Reap in the background; Wait must not run under c.mu because the
		// exiting reader goroutine's onProcessExit also takes it.
		cmd := c.cmd
		go func() {
			defer func() { _ = recover() }()
			_ = cmd.Wait()
		}()
	}
	c.cmd = nil
	c.ready = false
}

// onProcessExit runs when the reader goroutine sees stdout EOF. It fails all
// in-flight calls and marks the process dead so the next call respawns
// (subject to restartBackoff). A stale generation (already respawned) no-ops.
func (c *Client) onProcessExit(gen int) {
	c.mu.Lock()
	if gen != c.generation {
		c.mu.Unlock()
		return
	}
	wasClosed := c.closed
	c.stopLocked()
	c.mu.Unlock()

	err := errors.New("mcp server process exited")
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		ch <- rpcResponse{Err: err}
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()

	if !wasClosed && c.lifeCtx.Err() == nil {
		// Not Close() and not a lifeCtx (gateway shutdown) kill: the toolset
		// silently degrades until the next call respawns the child —
		// operator-visible per logging.md. The lifeCtx check keeps clean
		// shutdowns/restarts from logging a false-positive Error.
		c.logger.Error("mcp server process exited unexpectedly", "cmd", c.argv[0])
	}
}

// readLoop parses newline-delimited JSON-RPC messages from stdout.
func (c *Client) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			c.logger.Warn("mcp server sent unparseable line", "error", err, "bytes", len(line))
			continue
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			// Server-initiated request (sampling/roots/…) — out of scope.
			c.logger.Debug("mcp server request unsupported", "method", msg.Method)
			_ = c.send(map[string]any{
				"jsonrpc": "2.0",
				"id":      *msg.ID,
				"error":   rpcError{Code: -32601, Message: "method not supported by deneb-gateway client"},
			})
		case msg.Method != "":
			// Notification — ignored (Debug to avoid per-event spam).
			c.logger.Debug("mcp server notification ignored", "method", msg.Method)
		case msg.ID != nil:
			var resp rpcResponse
			if msg.Error != nil {
				resp.Err = fmt.Errorf("mcp server error %d: %s", msg.Error.Code, msg.Error.Message)
			} else {
				resp.Result = append(json.RawMessage(nil), msg.Result...)
			}
			c.pendingMu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- resp // buffered; never blocks the reader
			}
		}
	}
	if err := sc.Err(); err != nil {
		c.logger.Warn("mcp stdout read ended", "error", err)
	}
}

// roundTrip sends a request and waits for its response or ctx expiry.
func (c *Client) roundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.pendingMu.Lock()
	c.nextID++
	id := c.nextID
	// Buffered so a late response after ctx expiry never blocks the reader.
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.send(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Err != nil {
			return nil, fmt.Errorf("mcpclient: %s: %w", method, resp.Err)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("mcpclient: %s: %w", method, ctx.Err())
	}
}

// send marshals and writes one newline-terminated message to the child's stdin.
func (c *Client) send(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcpclient: marshal: %w", err)
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return errors.New("mcpclient: server process not running")
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("mcpclient: write: %w", err)
	}
	return nil
}
