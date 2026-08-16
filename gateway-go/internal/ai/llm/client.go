package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const llmResponseHeaderTimeout = 20 * time.Minute

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
	// Streaming providers normally send headers immediately, but Complete uses
	// a non-streaming response and cloud models may not emit headers until a
	// long prefill + generation finishes. Aurora Dream's GLM synthesis crossed
	// the old 2-minute boundary three times and failed after 363 seconds even
	// though its caller budget was longer. Keep the transport ceiling above the
	// long-call budgets; caller contexts and the stream idle watchdog remain
	// the task-specific hang guards.
	ResponseHeaderTimeout: llmResponseHeaderTimeout,
	ForceAttemptHTTP2:     true,
}

// API mode constants for Client. Controls request/response wire format.
//
// APIModeOpenAI: POST /chat/completions with OpenAI JSON, OpenAI SSE.
// APIModeAnthropic: POST /v1/messages with Anthropic JSON, Anthropic SSE.
const (
	APIModeOpenAI    = "openai"
	APIModeAnthropic = "anthropic"
)

// Auth scheme constants for Anthropic Messages requests. Controls how the
// credential is presented.
//
// AuthSchemeXAPIKey: the `x-api-key` header (Anthropic / Z.ai default).
// AuthSchemeBearer:  the `Authorization: Bearer` header — used by
// OAuth-token endpoints (Kimi Code, MiMo Token Plan).
const (
	AuthSchemeXAPIKey = "x-api-key"
	AuthSchemeBearer  = "bearer"
)

// Client is an HTTP client for LLM provider APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	logger     *slog.Logger
	apiMode    string // "openai" (default) or "anthropic"

	// authScheme controls how the credential is sent on Anthropic Messages
	// requests: "" / "x-api-key" (default) or "bearer".
	authScheme string

	// apiKeyFunc, when set, supplies the API key per request, overriding
	// the static apiKey when it returns a non-empty value. Used for
	// credentials that rotate on disk (e.g. the Kimi CLI's token cache).
	apiKeyFunc func() string

	// extraHeaders are applied to every outgoing request (from a provider
	// config). Used for endpoint-required headers like a custom User-Agent.
	extraHeaders map[string]string

	// Retry configuration.
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	// rateLimitMaxRetries caps retries for 429 rate-limit / "overloaded"
	// responses specifically. A persistently overloaded provider won't clear
	// within the retry window, so retrying it 6× (≈128s of backoff) only burns
	// the turn budget and leaves nothing for the caller's model-fallback chain.
	// After this many rate-limit retries the error is surfaced so a DIFFERENT
	// model can be tried while budget remains. Other transient errors (5xx,
	// timeouts) still use the full maxRetries.
	rateLimitMaxRetries int

	// minRequestTimeout is the minimum time each individual LLM HTTP request
	// gets, regardless of how much of the agent-level deadline remains. When
	// the parent context's remaining deadline is shorter than this value, a
	// derived context with a fresh timeout is created (still cancellable via
	// the parent for agent abort).
	minRequestTimeout time.Duration
	// maxStreamBytes caps raw provider SSE payload bytes before protocol
	// translation. Zero preserves the production unlimited aggregate behavior.
	maxStreamBytes int
}

// ClientOption configures a Client.
type ClientOption func(*Client)

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

// APIMode reports the wire protocol this client speaks — APIModeOpenAI or
// APIModeAnthropic. Callers use it to gate behavior that is only safe on one
// protocol (e.g. enabling extended thinking only on Anthropic Messages mode,
// where reasoning arrives as distinct SSE thinking blocks rather than leaking
// into the answer body).
func (c *Client) APIMode() string { return c.apiMode }

// BaseURL returns the client's resolved base URL. The chat pipeline uses it
// to locate a self-hosted vLLM engine's /metrics endpoint for prefix-cache
// telemetry; it carries no credentials.
func (c *Client) BaseURL() string { return c.baseURL }

// CloneForDeterministicRun returns an isolated client profile for bounded
// evaluation. It preserves endpoint/auth/wire settings while disabling
// redirects, retries with jitter, and the production minimum-request timeout
// that can intentionally outlive a short parent deadline.
func (c *Client) CloneForDeterministicRun() *Client {
	if c == nil {
		return nil
	}
	clone := *c
	if c.httpClient != nil {
		httpClone := *c.httpClient
		httpClone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		clone.httpClient = &httpClone
	}
	clone.maxRetries = 0
	clone.baseDelay = 0
	clone.maxDelay = 0
	clone.minRequestTimeout = 0
	clone.maxStreamBytes = 8 << 20
	if c.extraHeaders != nil {
		clone.extraHeaders = make(map[string]string, len(c.extraHeaders))
		for key, value := range c.extraHeaders {
			clone.extraHeaders[key] = value
		}
	}
	return &clone
}

// WithHeaders sets extra HTTP headers applied to every request this
// client makes. Provider configs use this for endpoint-required headers
// (e.g. a custom User-Agent). Values overwrite client defaults, so a
// header here can replace the default User-Agent. The map is copied.
func WithHeaders(h map[string]string) ClientOption {
	return func(cl *Client) {
		if len(h) == 0 {
			return
		}
		cl.extraHeaders = make(map[string]string, len(h))
		for k, v := range h {
			cl.extraHeaders[k] = v
		}
	}
}

// WithAuthScheme selects how the credential is sent on Anthropic Messages
// requests: "x-api-key" (default) or "bearer" (Authorization: Bearer, for
// OAuth-token providers). Ignored for OpenAI-mode clients, which always
// use a Bearer Authorization header.
func WithAuthScheme(scheme string) ClientOption {
	return func(cl *Client) {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case AuthSchemeBearer:
			cl.authScheme = AuthSchemeBearer
		case AuthSchemeXAPIKey:
			cl.authScheme = AuthSchemeXAPIKey
		}
	}
}

// WithAPIKeyFunc sets a callback that supplies the API key per request,
// overriding the static key whenever it returns a non-empty value. Used
// for credentials that rotate on disk (e.g. the Kimi CLI's OAuth token
// cache) so a refreshed token is picked up without restarting the gateway.
func WithAPIKeyFunc(fn func() string) ClientOption {
	return func(cl *Client) { cl.apiKeyFunc = fn }
}

// resolveAPIKey returns the credential for the next request, preferring
// the dynamic apiKeyFunc (when set and non-empty) over the static key.
func (c *Client) resolveAPIKey() string {
	if c.apiKeyFunc != nil {
		if k := strings.TrimSpace(c.apiKeyFunc()); k != "" {
			return k
		}
	}
	return strings.TrimSpace(c.apiKey)
}

// applyHeaders sets the honest default User-Agent and any client-configured
// extra headers on req. Extra headers are applied last so a provider config
// can override any default, including the User-Agent — required by some
// coding-subscription endpoints that gate access on the client identifier.
func (c *Client) applyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", httputil.UserAgent())
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
}

// NewClient creates a new LLM API client.
//
// The 30-minute client timeout caps the WHOLE exchange including the streamed
// body read, so it must exceed the longest legitimate stream: a local model at
// ~40 tok/s producing a max_tokens-recovery-scaled output (1.5×16384 tokens)
// plus long-context prefill already passes 10 minutes. Genuine hangs are
// caught much earlier by ResponseHeaderTimeout (wedged server) and the
// caller-level stream idle watchdog (no event for 180s — see
// agent.consumeStreamInto); this is only the last-resort backstop.
func NewClient(baseURL, apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		httpClient:          &http.Client{Timeout: 30 * time.Minute, Transport: sharedTransport},
		baseURL:             baseURL,
		apiKey:              apiKey,
		logger:              slog.Default(),
		apiMode:             APIModeOpenAI,
		maxRetries:          6,
		baseDelay:           1 * time.Second,
		maxDelay:            60 * time.Second,
		rateLimitMaxRetries: 3,
		minRequestTimeout:   5 * time.Minute,
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
	// nil when the caller did not opt in (helper calls, tests) — every record
	// is then a no-op, so this observability can never change control flow.
	collector := retryCollectorFrom(ctx)
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Agent-level context expired — retrying won't help since
			// the deadline won't extend.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("agent context expired: %w", lastErr)
			}

			// A persistently rate-limited / overloaded provider won't clear
			// within the retry window. Surface the error after a few tries so the
			// caller's model-fallback chain can switch models while the turn still
			// has budget — otherwise a 429 storm burns ~128s over 6 retries and
			// the turn times out with no output (observed with kimi overload).
			if c.rateLimitMaxRetries > 0 && attempt > c.rateLimitMaxRetries && isRetryableRateLimit(lastErr) {
				return nil, lastErr
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
			// Status-less failure: the provider never answered. Recorded under
			// its own label so a transport storm cannot be mistaken for a
			// rate-limit storm when reading the ledger.
			collector.record(retryAttempt{kind: "transport"})
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
		// Recorded before the retryability check so a permanent failure still
		// appears in the turn's failure mix — "one 400" and "no failures" are
		// very different rows to read.
		collector.record(retryAttempt{status: resp.StatusCode})

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
	if errors.Is(parent.Err(), context.Canceled) {
		return parent, func() {}
	}

	// Parent deadline is too tight. Create a derived context with the
	// minimum timeout.
	child, cancel := context.WithTimeout(context.Background(), c.minRequestTimeout)

	// Propagate explicit cancellation (agent abort), but not the parent
	// deadline we intentionally extended for this single HTTP request.
	stop := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.Canceled) {
			cancel()
		}
	})
	return child, func() { stop(); cancel() }
}

// cancelOnClose wraps an io.ReadCloser to call a cancel function on Close.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

// Close releases the wrapped resource.
func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// backoffDelay computes exponential backoff with jitter, respecting
// Retry-After headers. 429 rate limits use a higher base delay floor.
func (c *Client) backoffDelay(attempt int, err error) time.Duration {
	// Respect Retry-After header from the API, clamped to the configured max
	// so a large (or hostile) header value cannot stall the retry loop.
	var apiErr *httpretry.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		if c.maxDelay > 0 && apiErr.RetryAfter > c.maxDelay {
			return c.maxDelay
		}
		return apiErr.RetryAfter
	}

	base := c.baseDelay
	// Rate-limited responses need a higher floor than transient server errors.
	if apiErr != nil &&
		httpretry.Classify(apiErr.StatusCode) == httpretry.CategoryRateLimit {
		const rateLimitFloor = 2 * time.Second
		if base < rateLimitFloor {
			base = rateLimitFloor
		}
	}

	return httpretry.Backoff{Base: base, Max: c.maxDelay, Jitter: 0.25}.Delay(attempt)
}

// parseRetryAfter parses the Retry-After header value. RFC 9110 allows both
// delay-seconds and an HTTP-date; some providers send the date form, which the
// previous seconds-only parse silently dropped (losing the server's explicit
// pacing and falling back to blind exponential backoff).
func parseRetryAfter(val string) time.Duration {
	// RFC 9110 permits optional whitespace around field values; without the
	// trim, " 120" fails Atoi and a CRLF-tailed date fails ParseTime.
	val = strings.TrimSpace(val)
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// isProviderPermanentRateLimit returns true for provider error payloads that
// represent hard request-capacity limits where immediate retry is unlikely to
// succeed (e.g. OpenRouter code 1302: "Rate limit reached for requests").
func isProviderPermanentRateLimit(err error) bool {
	var apiErr *httpretry.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests || apiErr.Message == "" {
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

// isRetryableRateLimit reports whether err is a 429 rate-limit / overload
// response (the transient, retry-worthy kind — permanent ones are surfaced
// earlier). Used to cap rate-limit retries short of the full maxRetries so the
// caller can fail over to another model before the turn budget is spent.
func isRetryableRateLimit(err error) bool {
	var apiErr *httpretry.APIError
	return errors.As(err, &apiErr) &&
		httpretry.Classify(apiErr.StatusCode) == httpretry.CategoryRateLimit
}
