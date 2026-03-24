// Package vega implements an MCP client for communicating with the Python
// Vega project management tool via subprocess + JSONL protocol.
//
// This mirrors the existing Node.js → Python integration but runs directly
// from the Go gateway, eliminating the Node.js hop for Vega operations.
package vega

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultTimeout is the maximum time to wait for a Vega response.
	defaultTimeout = 30 * time.Second
)

// Client manages communication with the Vega Python subprocess.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	mu      sync.Mutex // serializes writes
	pending sync.Map   // reqID -> chan *Response
	nextID  atomic.Int64
	logger  *slog.Logger
	done    chan struct{}
}

// ToolCall represents a Vega MCP tool invocation.
type ToolCall struct {
	Tool   string          `json:"tool"`
	Params json.RawMessage `json:"params"`
}

// Request is a JSONL MCP request to the Vega subprocess.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is a JSONL MCP response from the Vega subprocess.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError is the error shape in a JSONRPC response.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Config configures the Vega client.
type Config struct {
	// Command is the shell command to start the Vega subprocess.
	// Defaults to "bash vega/vega-wrapper.sh".
	Command string
	// WorkDir is the working directory for the subprocess.
	WorkDir string
	// Env is additional environment variables.
	Env map[string]string
	// Logger for client messages.
	Logger *slog.Logger
}

// New creates and starts a Vega client subprocess.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	command := cfg.Command
	if command == "" {
		command = "bash vega/vega-wrapper.sh"
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	// Set environment.
	if len(cfg.Env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start vega subprocess: %w", err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
		logger: cfg.Logger,
		done:   make(chan struct{}),
	}

	go c.readLoop()

	cfg.Logger.Info("vega client started", "pid", cmd.Process.Pid)
	return c, nil
}

// Call invokes a Vega MCP tool and returns the result.
func (c *Client) Call(ctx context.Context, tool string, params json.RawMessage) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	// Build JSONRPC request.
	toolParams := struct {
		Name   string          `json:"name"`
		Params json.RawMessage `json:"arguments"`
	}{
		Name:   tool,
		Params: params,
	}

	paramsBytes, err := json.Marshal(toolParams)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	respCh := make(chan *Response, 1)
	c.pending.Store(id, respCh)
	defer c.pending.Delete(id)

	// Write request as JSONL.
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	_, writeErr := fmt.Fprintf(c.stdin, "%s\n", data)
	c.mu.Unlock()
	if writeErr != nil {
		return nil, fmt.Errorf("write to vega: %w", writeErr)
	}

	// Wait for response with timeout.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("vega error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("vega subprocess exited")
	}
}

// Close stops the Vega subprocess.
func (c *Client) Close() error {
	close(c.done)
	_ = c.stdin.Close()
	return c.cmd.Wait()
}

// readLoop reads JSONL responses from the Vega subprocess.
func (c *Client) readLoop() {
	defer func() {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}()

	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			c.logger.Error("vega: invalid JSONL response", "error", err, "line", string(line[:min(len(line), 200)]))
			continue
		}

		if ch, ok := c.pending.Load(resp.ID); ok {
			respCh := ch.(chan *Response)
			select {
			case respCh <- &resp:
			default:
			}
		}
	}

	if err := c.stdout.Err(); err != nil {
		c.logger.Error("vega: read error", "error", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
