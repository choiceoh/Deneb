package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Client is an HTTP client for LLM provider APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	logger     *slog.Logger

	// Retry configuration.
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
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

// NewClient creates a new LLM API client.
func NewClient(baseURL, apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 10 * time.Minute},
		baseURL:    baseURL,
		apiKey:     apiKey,
		logger:     slog.Default(),
		maxRetries: 3,
		baseDelay:  1 * time.Second,
		maxDelay:   60 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DoStream sends an HTTP request and returns the response body for streaming.
// The caller is responsible for closing the returned ReadCloser.
// Retries on transient errors (429, 500, 502, 503, 529).
func (c *Client) DoStream(ctx context.Context, req *http.Request) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.backoffDelay(attempt, lastErr)
			c.logger.Info("retrying LLM request",
				"attempt", attempt, "delay", delay, "error", lastErr)
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

		resp, err := c.httpClient.Do(req.WithContext(ctx))
		if err != nil {
			lastErr = fmt.Errorf("http request failed: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.Body, nil
		}

		// Read error body for diagnostics.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		lastErr = &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}

		// Only retry on transient errors.
		if !isRetryable(resp.StatusCode) {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// backoffDelay computes exponential backoff, respecting Retry-After headers.
func (c *Client) backoffDelay(attempt int, err error) time.Duration {
	// Check for Retry-After from API error.
	if apiErr, ok := err.(*APIError); ok && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	delay := time.Duration(float64(c.baseDelay) * math.Pow(2, float64(attempt-1)))
	if delay > c.maxDelay {
		delay = c.maxDelay
	}
	return delay
}

// isRetryable returns true for HTTP status codes that warrant a retry.
func isRetryable(status int) bool {
	switch status {
	case 429, 500, 502, 503, 529:
		return true
	default:
		return false
	}
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

// APIError represents a non-2xx response from the LLM API.
type APIError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("LLM API error %d: %s", e.StatusCode, e.Body)
}
