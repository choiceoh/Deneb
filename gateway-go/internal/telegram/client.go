package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const apiBaseURL = "https://api.telegram.org/bot"

const (
	defaultClientTimeout = 60 * time.Second
	defaultMaxRetries    = 3
	defaultRetryBase     = 1 * time.Second
	defaultRetryMax      = 30 * time.Second
)

// ClientConfig configures the Telegram Bot API client.
type ClientConfig struct {
	// Token is the bot API token.
	Token string
	// HTTPClient overrides the default HTTP client (for proxy support, custom timeouts).
	// If nil, a client with IPv4-fallback transport is created automatically.
	HTTPClient *http.Client
	// Logger for client operations. If nil, slog.Default() is used.
	Logger *slog.Logger
	// MaxRetries is the maximum number of retries for transient errors (default 3).
	MaxRetries *int
}

// Client is a thin wrapper around the Telegram Bot API.
// It includes retry with exponential backoff and IPv4-fallback transport.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	maxRetries int
}

// NewClient creates a new Telegram Bot API client.
// By default it uses a custom transport with IPv4 fallback and keepalive.
func NewClient(cfg ClientConfig) *Client {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default().With("pkg", "telegram")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = NewTelegramHTTPClient(defaultClientTimeout, logger)
	}

	maxRetries := defaultMaxRetries
	if cfg.MaxRetries != nil {
		maxRetries = *cfg.MaxRetries
	}

	return &Client{
		token:      cfg.Token,
		baseURL:    apiBaseURL + cfg.Token,
		httpClient: httpClient,
		logger:     logger,
		maxRetries: maxRetries,
	}
}

// Call makes a JSON POST request to the Bot API with automatic retry.
// Idempotent methods (getMe, getUpdates, etc.) retry on all network errors.
// Non-idempotent methods (sendMessage, etc.) only retry on pre-connect errors.
//
// Use CallIdempotent for explicit idempotent retry, or Call for the default
// behavior (conservative: only retries on pre-connect errors).
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.callWithRetry(ctx, method, params, false)
}

// CallIdempotent makes a JSON POST request with idempotent retry behavior.
// Retries on all recoverable network errors, not just pre-connect errors.
func (c *Client) CallIdempotent(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.callWithRetry(ctx, method, params, true)
}

func (c *Client) callWithRetry(ctx context.Context, method string, params any, idempotent bool) (json.RawMessage, error) {
	// Serialize params once.
	var paramData []byte
	if params != nil {
		var err error
		paramData, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, lastErr)
			c.logger.Info("retrying telegram API call",
				"method", method,
				"attempt", attempt,
				"delay", delay,
				"error", lastErr,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var body io.Reader
		if paramData != nil {
			body = bytes.NewReader(paramData)
		}

		result, err := c.doCall(ctx, method, body)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// API errors (4xx/5xx from Telegram) are not retried here.
		// The bot-level backoff in bot.go handles polling retries.
		// Only transport-level errors (network failures) are retried.
		var apiErr *APIError
		if isAPIError(err, &apiErr) {
			return nil, err
		}

		// Decide whether to retry based on idempotency.
		if idempotent {
			if !IsNetworkError(err) {
				return nil, err
			}
		} else {
			if !IsPreConnectError(err) {
				return nil, err
			}
		}
	}

	return nil, fmt.Errorf("telegram %s: max retries exceeded: %w", method, lastErr)
}

// doCall executes a single HTTP request (no retry).
func (c *Client) doCall(ctx context.Context, method string, body io.Reader) (json.RawMessage, error) {
	url := c.baseURL + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w (status %d)", err, resp.StatusCode)
	}

	if !apiResp.OK {
		return nil, &APIError{
			Code:        apiResp.ErrorCode,
			Description: apiResp.Description,
			RetryAfter:  retryAfterFromParams(apiResp.Parameters),
		}
	}

	return apiResp.Result, nil
}

// Upload sends a multipart/form-data request for file uploads with automatic retry.
// Retries only on pre-connect errors (uploads are non-idempotent).
func (c *Client) Upload(ctx context.Context, method string, fieldName string, fileName string, fileData io.Reader, params map[string]string) (json.RawMessage, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Add file field.
	part, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, fileData); err != nil {
		return nil, fmt.Errorf("copy file data: %w", err)
	}

	// Add other params as form fields.
	for k, v := range params {
		if err := w.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("write field %s: %w", k, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	return c.uploadWithRetry(ctx, method, buf.Bytes(), w.FormDataContentType())
}

// doUpload executes a single upload HTTP request (no retry).
func (c *Client) doUpload(ctx context.Context, method string, body []byte, contentType string) (json.RawMessage, error) {
	url := c.baseURL + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read upload response: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode upload response: %w (status %d)", err, resp.StatusCode)
	}

	if !apiResp.OK {
		return nil, &APIError{
			Code:        apiResp.ErrorCode,
			Description: apiResp.Description,
			RetryAfter:  retryAfterFromParams(apiResp.Parameters),
		}
	}

	return apiResp.Result, nil
}

// uploadWithRetry wraps doUpload with the same retry logic as callWithRetry.
// Only retries on pre-connect errors since uploads are non-idempotent.
func (c *Client) uploadWithRetry(ctx context.Context, method string, body []byte, contentType string) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, lastErr)
			c.logger.Info("retrying telegram upload",
				"method", method,
				"attempt", attempt,
				"delay", delay,
				"error", lastErr,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := c.doUpload(ctx, method, body, contentType)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// API errors are not retried.
		var apiErr *APIError
		if isAPIError(err, &apiErr) {
			return nil, err
		}

		// Only retry pre-connect errors (upload is non-idempotent).
		if !IsPreConnectError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("telegram upload %s: max retries exceeded: %w", method, lastErr)
}

// --- Convenience methods ---

// GetMe calls the getMe API method to verify the bot token.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	result, err := c.CallIdempotent(ctx, "getMe", nil)
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(result, &user); err != nil {
		return nil, fmt.Errorf("decode getMe: %w", err)
	}
	return &user, nil
}

// GetUpdates fetches incoming updates using long polling.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	params := map[string]any{
		"offset":  offset,
		"timeout": timeout,
		"allowed_updates": []string{
			"message", "edited_message", "channel_post",
			"callback_query", "message_reaction",
		},
	}
	// Long polling needs generous deadline.
	pollCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second+10*time.Second)
		defer cancel()
	}
	result, err := c.CallIdempotent(pollCtx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, fmt.Errorf("decode getUpdates: %w", err)
	}
	return updates, nil
}

// SendChatAction sends a chat action (e.g. "typing").
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	_, err := c.CallIdempotent(ctx, "sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  action,
	})
	return err
}

// SetMessageReaction sets an emoji reaction on a message.
// Pass an empty emoji to remove all reactions.
func (c *Client) SetMessageReaction(ctx context.Context, chatID, messageID int64, emoji string) error {
	var reaction []map[string]string
	if emoji != "" {
		reaction = []map[string]string{{"type": "emoji", "emoji": emoji}}
	}
	_, err := c.CallIdempotent(ctx, "setMessageReaction", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"reaction":   reaction,
	})
	return err
}

// AnswerCallbackQuery sends a response to a callback query.
func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackQueryID, text string) error {
	_, err := c.CallIdempotent(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackQueryID,
		"text":              text,
	})
	return err
}

// GetFile retrieves file metadata (including download path) for a given file_id.
func (c *Client) GetFile(ctx context.Context, fileID string) (*File, error) {
	result, err := c.CallIdempotent(ctx, "getFile", map[string]any{
		"file_id": fileID,
	})
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(result, &f); err != nil {
		return nil, fmt.Errorf("decode getFile: %w", err)
	}
	return &f, nil
}

// FileDownloadURL returns the direct download URL for a Telegram file.
// The filePath comes from GetFile().FilePath.
func (c *Client) FileDownloadURL(filePath string) string {
	return "https://api.telegram.org/file/bot" + c.token + "/" + filePath
}

// DownloadFile downloads a file from Telegram by file_id.
// Returns the raw bytes and the file path (useful for MIME detection from extension).
func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	f, err := c.GetFile(ctx, fileID)
	if err != nil {
		return nil, "", fmt.Errorf("getFile: %w", err)
	}
	if f.FilePath == "" {
		return nil, "", fmt.Errorf("file has no download path (may exceed 20 MB)")
	}

	url := c.FileDownloadURL(f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Limit to 50 MB (Telegram's max file size for bots).
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, "", fmt.Errorf("read file data: %w", err)
	}
	return data, f.FilePath, nil
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	_, err := c.CallIdempotent(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	})
	return err
}

// APIError represents a Telegram Bot API error response.
type APIError struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram API error %d: %s", e.Code, e.Description)
}

// IsRetryable returns true if the error suggests the request can be retried.
func (e *APIError) IsRetryable() bool {
	return e.Code == 429 || e.Code >= 500
}

// IsParseError returns true if the error is an HTML/entity parsing failure.
func (e *APIError) IsParseError() bool {
	return e.Code == 400 && (strings.Contains(e.Description, "can't parse entities") ||
		strings.Contains(e.Description, "parse entities") ||
		strings.Contains(e.Description, "find end of the entity"))
}

// IsThreadNotFound returns true if the error is about a missing message thread.
func (e *APIError) IsThreadNotFound() bool {
	return e.Code == 400 && strings.Contains(e.Description, "message thread not found")
}

// IsRateLimited returns true if the error is a rate limit (429).
func (e *APIError) IsRateLimited() bool {
	return e.Code == 429
}

func retryAfterFromParams(p *ResponseParameters) int {
	if p != nil {
		return p.RetryAfter
	}
	return 0
}

// retryDelay computes exponential backoff, respecting Retry-After from Telegram API errors.
func retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if isAPIError(err, &apiErr) && apiErr.RetryAfter > 0 {
		return time.Duration(apiErr.RetryAfter) * time.Second
	}
	delay := time.Duration(float64(defaultRetryBase) * math.Pow(2, float64(attempt-1)))
	if delay > defaultRetryMax {
		delay = defaultRetryMax
	}
	return delay
}

// isAPIError checks if err is or wraps an *APIError.
func isAPIError(err error, target **APIError) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*APIError)
	if ok {
		*target = e
		return true
	}
	// Check wrapped errors.
	type wrapper interface{ Unwrap() error }
	if w, ok := err.(wrapper); ok {
		return isAPIError(w.Unwrap(), target)
	}
	return false
}
