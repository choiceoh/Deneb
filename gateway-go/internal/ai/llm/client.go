package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

// sharedTransport is a connection-pooled HTTP transport shared across all
// LLM clients. Avoids per-request TCP/TLS handshake overhead by reusing
// idle connections. Tuned for DGX Spark single-user deployment where most
// requests go to 1-2 provider endpoints.
var sharedTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        64,
	MaxIdleConnsPerHost: 16,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 5 * time.Second,
	ForceAttemptHTTP2:   true,
}

// API mode constants for Client. Controls request/response wire format.
//
// APIModeOpenAI: POST /chat/completions with OpenAI JSON, OpenAI SSE.
// APIModeAnthropic: POST /v1/messages with Anthropic JSON, Anthropic SSE.
const (
	APIModeOpenAI    = "openai"
	APIModeAnthropic = "anthropic"
)

// Client is an HTTP client for LLM provider APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	logger     *slog.Logger
	apiMode    string // "openai" (default) or "anthropic"

	// Retry configuration.
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration

	// minRequestTimeout is the minimum time each individual LLM HTTP request
	// gets, regardless of how much of the agent-level deadline remains. When
	// the parent context's remaining deadline is shorter than this value, a
	// derived context with a fresh timeout is created (still cancellable via
	// the parent for agent abort).
	minRequestTimeout time.Duration
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) { cl.httpClient = c }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) ClientOption {
	return func(cl *Client) { cl.logger = l }
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, baseDelay, maxDelay time.Duration) ClientOption {
	return func(cl *Client) {
		cl.maxRetries = maxRetries
		cl.baseDelay = baseDelay
		cl.maxDelay = maxDelay
	}
}

// WithMinRequestTimeout sets the minimum per-request timeout. Each HTTP
// request will get at least this much time, even if the parent context's
// deadline has less remaining.
func WithMinRequestTimeout(d time.Duration) ClientOption {
	return func(cl *Client) { cl.minRequestTimeout = d }
}

// WithAPIMode selects the wire protocol the client speaks. Accepts
// "openai" (default — POST /chat/completions) or "anthropic" (POST
// /v1/messages with Anthropic Messages JSON). Unknown values are
// treated as "openai".
func WithAPIMode(mode string) ClientOption {
	return func(cl *Client) {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case APIModeAnthropic, "anthropic-messages":
			cl.apiMode = APIModeAnthropic
		default:
			cl.apiMode = APIModeOpenAI
		}
	}
}

// NewClient creates a new LLM API client.
func NewClient(baseURL, apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient:        &http.Client{Timeout: 10 * time.Minute, Transport: sharedTransport},
		baseURL:           baseURL,
		apiKey:            apiKey,
		logger:            slog.Default(),
		apiMode:           APIModeOpenAI,
		maxRetries:        6,
		baseDelay:         1 * time.Second,
		maxDelay:          60 * time.Second,
		minRequestTimeout: 5 * time.Minute,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DoStream sends an HTTP request and returns the response body for streaming.
// The caller is responsible for closing the returned ReadCloser.
// Retries on transient errors per httpretry.IsRetryable (rate limits, timeouts,
// server overload — never on permanent 4xx or 501).
func (c *Client) DoStream(ctx context.Context, req *http.Request) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Agent-level context expired — retrying won't help since
			// the deadline won't extend.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("agent context expired: %w", lastErr)
			}

			delay := c.backoffDelay(attempt, lastErr)
			attrs := []any{"attempt", attempt, "delay", delay, "error", lastErr, "url", req.URL.String()}
			if dl, ok := ctx.Deadline(); ok {
				attrs = append(attrs, "ctxRemaining", time.Until(dl).Truncate(time.Millisecond))
			}
			c.logger.Info("retrying LLM request", attrs...)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}

			// Reset the request body for retry. bytes.Reader implements
			// io.Seeker, so we can rewind it. For GetBody-enabled requests
			// (e.g. from http.NewRequest), recreate the body from GetBody.
			if seeker, ok := req.Body.(io.Seeker); ok {
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					return nil, fmt.Errorf("reset request body for retry: %w", err)
				}
			} else if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("recreate request body for retry: %w", err)
				}
				req.Body = body
			}
		}

		reqCtx, reqCancel := c.requestContext(ctx)
		resp, err := c.httpClient.Do(req.WithContext(reqCtx))
		if err != nil {
			reqCancel()
			lastErr = fmt.Errorf("http request failed: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Keep reqCancel alive — caller owns the response body.
			// Wrap body so cancelling happens on Close.
			return &cancelOnClose{ReadCloser: resp.Body, cancel: reqCancel}, nil
		}

		reqCancel()

		// Read error body for diagnostics.
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if readErr != nil {
			body = []byte("(failed to read error body)")
		}
		lastErr = &httpretry.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}

		// Only retry on transient errors (rate limit, timeout, server overload).
		if !httpretry.IsRetryable(resp.StatusCode) || isProviderPermanentRateLimit(lastErr) {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// requestContext returns a context for a single HTTP request. If the parent
// context's remaining deadline is less than minRequestTimeout, it creates a
// new context with a fresh timeout while still propagating parent cancellation
// (e.g., agent abort). Otherwise it returns the parent context as-is.
func (c *Client) requestContext(parent context.Context) (context.Context, context.CancelFunc) {
	if c.minRequestTimeout <= 0 {
		return parent, func() {}
	}
	dl, hasDL := parent.Deadline()
	if !hasDL || time.Until(dl) >= c.minRequestTimeout {
		return parent, func() {}
	}

	// Parent deadline is too tight. Create a derived context with the
	// minimum timeout.
	child, cancel := context.WithTimeout(context.Background(), c.minRequestTimeout)

	// If the parent is not yet done, propagate explicit cancellation
	// (agent abort) via AfterFunc. If the parent is already done (deadline
	// expired), skip AfterFunc — it would fire immediately and cancel the
	// fresh context we just created.
	if parent.Err() == nil {
		stop := context.AfterFunc(parent, func() { cancel() })
		return child, func() { stop(); cancel() }
	}
	return child, cancel
}

// cancelOnClose wraps an io.ReadCloser to call a cancel function on Close.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// backoffDelay computes exponential backoff with jitter, respecting
// Retry-After headers. 429 rate limits use a higher base delay floor.
func (c *Client) backoffDelay(attempt int, err error) time.Duration {
	// Respect Retry-After header from the API.
	if apiErr, ok := err.(*httpretry.APIError); ok && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}

	base := c.baseDelay
	// Rate-limited responses need a higher floor than transient server errors.
	if apiErr, ok := err.(*httpretry.APIError); ok &&
		httpretry.Classify(apiErr.StatusCode) == httpretry.CategoryRateLimit {
		const rateLimitFloor = 2 * time.Second
		if base < rateLimitFloor {
			base = rateLimitFloor
		}
	}

	return httpretry.Backoff{Base: base, Max: c.maxDelay, Jitter: 0.25}.Delay(attempt)
}

// parseRetryAfter parses the Retry-After header value as seconds.
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 0
	}
	secs, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// isProviderPermanentRateLimit returns true for provider error payloads that
// represent hard request-capacity limits where immediate retry is unlikely to
// succeed (e.g. OpenRouter code 1302: "Rate limit reached for requests").
func isProviderPermanentRateLimit(err error) bool {
	apiErr, ok := err.(*httpretry.APIError)
	if !ok || apiErr.StatusCode != http.StatusTooManyRequests || apiErr.Message == "" {
		return false
	}
	var payload struct {
		Error struct {
			Code any `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(apiErr.Message), &payload) != nil {
		return false
	}
	switch v := payload.Error.Code.(type) {
	case string:
		return v == "1302"
	case float64:
		return int(v) == 1302
	default:
		return false
	}
}
