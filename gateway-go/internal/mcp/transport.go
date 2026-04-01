package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// Transport handles stdio JSON-RPC communication.
// Reads from reader (stdin), writes to writer (stdout).
// All logging goes to stderr via slog.
type Transport struct {
	reader  *bufio.Reader
	writer  io.Writer
	writeMu sync.Mutex // serialize writes to stdout
	logger  *slog.Logger
}

// NewTransport creates a transport reading from r and writing to w.
func NewTransport(r io.Reader, w io.Writer, logger *slog.Logger) *Transport {
	return &Transport{
		reader: bufio.NewReader(r),
		writer: w,
		logger: logger,
	}
}

// ReadRequest reads the next JSON-RPC request from stdin.
// Returns io.EOF when stdin is closed.
func (t *Transport) ReadRequest() (*JSONRPCRequest, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC: %w", err)
	}
	t.logger.Debug("recv", "method", req.Method, "id", string(req.ID))
	return &req, nil
}

// WriteResponse writes a JSON-RPC response to stdout.
func (t *Transport) WriteResponse(resp *JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	data = append(data, '\n')
	_, err = t.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	t.logger.Debug("send", "id", string(resp.ID))
	return err
}

// WriteNotification writes a JSON-RPC notification to stdout.
func (t *Transport) WriteNotification(notif *Notification) error {
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	data = append(data, '\n')
	_, err = t.writer.Write(data)
	t.logger.Debug("notify", "method", notif.Method)
	return err
}

// SendRequest sends a JSON-RPC request from server to client (for sampling).
// Returns the raw bytes written. The caller must read the response separately.
func (t *Transport) SendRequest(req *JSONRPCRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	data = append(data, '\n')
	_, err = t.writer.Write(data)
	t.logger.Debug("send-req", "method", req.Method, "id", string(req.ID))
	return err
}

// MakeResponse creates a success response for the given request ID.
func MakeResponse(id json.RawMessage, result any) (*JSONRPCResponse, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}, nil
}

// MakeErrorResponse creates an error response.
func MakeErrorResponse(id json.RawMessage, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
}
