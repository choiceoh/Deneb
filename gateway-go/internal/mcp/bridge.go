package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Bridge is an HTTP client that forwards RPC calls to the Deneb gateway.
type Bridge struct {
	baseURL string
	token   string
	client  *http.Client
}

// BridgeConfig holds configuration for the bridge.
type BridgeConfig struct {
	GatewayURL string        // default: http://127.0.0.1:18789
	Token      string        // bearer token (resolved from env/file)
	Timeout    time.Duration // default: 30s
}

// NewBridge creates a new gateway bridge.
func NewBridge(cfg BridgeConfig) *Bridge {
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://127.0.0.1:18789"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Bridge{
		baseURL: strings.TrimRight(cfg.GatewayURL, "/"),
		token:   cfg.Token,
		client:  httputil.NewClient(cfg.Timeout),
	}
}

// gatewayRequest mirrors the gateway's RequestFrame wire format.
type gatewayRequest struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// gatewayResponse mirrors the gateway's ResponseFrame wire format.
type gatewayResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *gatewayError   `json:"error,omitempty"`
}

type gatewayError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Call invokes a gateway RPC method and returns the raw payload.
func (b *Bridge) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var rid [16]byte
	rand.Read(rid[:])
	reqID := hex.EncodeToString(rid[:])
	gReq := gatewayRequest{
		Type:   "req",
		ID:     reqID,
		Method: method,
		Params: params,
	}

	body, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.token)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway HTTP call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gResp gatewayResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !gResp.OK {
		msg := "unknown gateway error"
		if gResp.Error != nil {
			msg = fmt.Sprintf("[%s] %s", gResp.Error.Code, gResp.Error.Message)
		}
		return nil, fmt.Errorf("gateway error: %s", msg)
	}

	return gResp.Payload, nil
}

// HealthCheck verifies the gateway is reachable.
func (b *Bridge) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gateway unreachable at %s: %w", b.baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// ResolveToken resolves the gateway auth token from environment or file.
// Priority: DENEB_TOKEN → DENEB_GATEWAY_TOKEN → ~/.deneb/credentials/token
func ResolveToken() string {
	if t := os.Getenv("DENEB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("DENEB_GATEWAY_TOKEN"); t != "" {
		return t
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".deneb", "credentials", "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
