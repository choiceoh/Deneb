// Package httputil provides shared HTTP transports and client factories.
//
// All modules that need a plain HTTP client should use NewClient instead of
// creating their own &http.Client{Timeout: X}. This ensures:
//   - Connection pooling: idle TCP/TLS connections reused across modules
//   - Consistent User-Agent: all outbound requests identify as Deneb-Gateway
//   - Graceful shutdown: CloseIdle drains the shared pool
//
// Modules with special dialer requirements (SSRF-safe, Telegram IPv4
// fallback, LLM-tuned) keep their own transports.
package httputil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const defaultUserAgent = "Deneb-Gateway"

// version is the gateway build version, set via SetVersion at startup.
var version string

// SetVersion records the build version for the User-Agent header.
// Call once from main/bootstrap before any HTTP requests.
func SetVersion(v string) { version = v }

// sharedTransport is a connection-pooled HTTP transport shared across all
// standard (non-SSRF, non-Telegram) HTTP clients in the gateway.
var sharedTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        64,
	MaxIdleConnsPerHost: 8,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 5 * time.Second,
	ForceAttemptHTTP2:   true,
}

// sharedRoundTripper wraps sharedTransport with User-Agent injection.
var sharedRoundTripper http.RoundTripper = &uaTransport{base: sharedTransport}

// uaTransport injects a User-Agent header on outbound requests when the
// caller hasn't set one explicitly (e.g., web stealth fetcher sets its own).
type uaTransport struct {
	base http.RoundTripper
}

func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		ua := defaultUserAgent
		if version != "" {
			ua += "/" + version
		}
		req.Header.Set("User-Agent", ua)
	}
	return t.base.RoundTrip(req)
}

// NewClient returns an *http.Client backed by the shared pooled transport.
// The timeout applies per-request. Multiple clients share the same underlying
// connection pool, so creating many clients is cheap.
//
// All requests automatically include a "Deneb-Gateway/<version>" User-Agent
// unless the caller sets one explicitly.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sharedRoundTripper,
	}
}

// CloseIdle drains idle connections in the shared transport pool.
// Call during graceful shutdown to release resources.
func CloseIdle() {
	sharedTransport.CloseIdleConnections()
}

// WaitForHealth polls a health endpoint until it returns a non-5xx status
// or ctx is cancelled/expired. Callers control the deadline via context.
func WaitForHealth(ctx context.Context, url string, interval time.Duration) error {
	client := NewClient(3 * time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check at %s: %w", url, ctx.Err())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("health check at %s: %w", url, err)
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
	}
}
