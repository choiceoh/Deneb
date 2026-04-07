package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/timeouts"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// ConnectorConfig configures an HTTP connector for a provider API.
type ConnectorConfig struct {
	BaseURL   string            `json:"baseUrl"`
	APIKey    string            `json:"apiKey,omitempty"`
	AuthMode  string            `json:"authMode,omitempty"` // "api_key", "bearer", "oauth", "token", "none"
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMs int64             `json:"timeoutMs,omitempty"`
}

// Connector is a reusable HTTP client for provider API calls.
// It handles auth header injection, env-var expansion in headers,
// and configurable timeouts. All methods are safe for concurrent use.
type Connector struct {
	mu     sync.RWMutex
	client *http.Client
	config ConnectorConfig
	logger *slog.Logger
}

// NewConnector creates a new provider HTTP connector.
func NewConnector(cfg ConnectorConfig, logger *slog.Logger) *Connector {
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = timeouts.ProviderHTTP
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Connector{
		client: httputil.NewClient(timeout),
		config: cfg,
		logger: logger,
	}
}

// Do executes an HTTP request against the provider API.
// The path is appended to the configured BaseURL.
func (c *Connector) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	// Snapshot config under read lock.
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()

	url := strings.TrimRight(cfg.BaseURL, "/")
	if path != "" {
		url += "/" + strings.TrimLeft(path, "/")
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("connector: build request: %w", err)
	}

	// Inject auth header based on mode.
	applyAuth(req, cfg.APIKey, cfg.AuthMode)

	// Apply custom headers with env-var expansion.
	for k, v := range cfg.Headers {
		req.Header.Set(k, ExpandEnvVars(v))
	}

	// Default content type for requests with bodies.
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connector: request %s %s: %w", method, path, err)
	}
	return resp, nil
}

// JSON executes a JSON request and decodes the response into respBody.
// If reqBody is non-nil it is JSON-encoded as the request body.
// If respBody is non-nil the response body is JSON-decoded into it.
func (c *Connector) JSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("connector: marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			errBody = []byte("(failed to read error body)")
		}
		return &ConnectorError{
			StatusCode: resp.StatusCode,
			Method:     method,
			Path:       path,
			Body:       string(errBody),
		}
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("connector: decode response: %w", err)
		}
	}
	return nil
}

// UpdateConfig replaces the connector's configuration (e.g., after key rotation).
func (c *Connector) UpdateConfig(cfg ConnectorConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = cfg
	if cfg.TimeoutMs > 0 {
		c.client.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
}

// applyAuth injects the appropriate authorization header.
func applyAuth(req *http.Request, key, authMode string) {
	if key == "" {
		return
	}
	switch strings.ToLower(authMode) {
	case "bearer", "oauth", "token":
		req.Header.Set("Authorization", "Bearer "+key)
	case "api_key":
		req.Header.Set("x-api-key", key)
	case "none", "":
		// No auth header.
	default:
		// Default to Bearer for unknown modes.
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

// ConnectorError represents a non-2xx HTTP response from a provider.
type ConnectorError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *ConnectorError) Error() string {
	return fmt.Sprintf("provider %s %s returned HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// ExpandEnvVars expands ${VAR} references in a string using os.Getenv.
// Both ${VAR} and $VAR forms are expanded.
func ExpandEnvVars(s string) string {
	return os.Expand(s, os.Getenv)
}
