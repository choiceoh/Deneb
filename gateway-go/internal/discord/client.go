package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultClientTimeout = 30 * time.Second
	defaultMaxRetries    = 3
)

// Client is a thin wrapper around the Discord REST API.
type Client struct {
	token      string
	httpClient *http.Client
	logger     *slog.Logger
	maxRetries int
}

// NewClient creates a new Discord REST API client.
func NewClient(token string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default().With("pkg", "discord")
	}
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: defaultClientTimeout,
		},
		logger:     logger,
		maxRetries: defaultMaxRetries,
	}
}

// doRequest executes an HTTP request with Discord auth headers and automatic
// rate limit retry. On 429 responses, it waits for the Retry-After duration
// and retries up to maxRetries times.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	// If body is a Reader, we need to be able to replay it for retries.
	// Buffer it if needed.
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, lastErr)
			c.logger.Info("retrying discord API call",
				"method", method, "path", path,
				"attempt", attempt, "delay", delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}

		result, err := c.doRequestOnce(ctx, method, path, reqBody, contentType)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Only retry on rate limits (429) and server errors (5xx).
		var apiErr *APIError
		if isDiscordAPIError(err, &apiErr) {
			if apiErr.IsRateLimited() || apiErr.StatusCode >= 500 {
				continue
			}
			return nil, err // Non-retryable API error.
		}
		// Network errors are retryable.
	}

	return nil, fmt.Errorf("discord %s %s: max retries exceeded: %w", method, path, lastErr)
}

// doRequestOnce executes a single HTTP request (no retry).
func (c *Client) doRequestOnce(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	url := BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "DiscordBot (deneb, 1.0)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
		// Parse Retry-After header for rate limits.
		if resp.StatusCode == 429 {
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.ParseFloat(ra, 64); err == nil {
					apiErr.RetryAfter = time.Duration(secs * float64(time.Second))
				}
			}
		}
		return nil, apiErr
	}

	return respBody, nil
}

// retryDelay computes the delay before retrying, respecting Retry-After from 429s.
func retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if isDiscordAPIError(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	// Exponential backoff: 1s, 2s, 4s, 8s capped at 15s.
	delay := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
	if delay > 15*time.Second {
		delay = 15 * time.Second
	}
	return delay
}

// isDiscordAPIError checks if err is or wraps an *APIError.
func isDiscordAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}

// Call makes a JSON request to the Discord REST API.
func (c *Client) Call(ctx context.Context, method, path string, params any) (json.RawMessage, error) {
	var body io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		body = bytes.NewReader(data)
	}

	var contentType string
	if body != nil {
		contentType = "application/json"
	}

	respBody, err := c.doRequest(ctx, method, path, body, contentType)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(respBody), nil
}

// --- Convenience methods ---

// GetCurrentUser calls GET /users/@me to verify the bot token.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	result, err := c.Call(ctx, http.MethodGet, "/users/@me", nil)
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(result, &user); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &user, nil
}

// SendMessage sends a message to a channel.
func (c *Client) SendMessage(ctx context.Context, channelID string, req *SendMessageRequest) (*Message, error) {
	result, err := c.Call(ctx, http.MethodPost, "/channels/"+channelID+"/messages", req)
	if err != nil {
		return nil, fmt.Errorf("sendMessage: %w", err)
	}
	var msg Message
	if err := json.Unmarshal(result, &msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	return &msg, nil
}

// SendMessageWithFile sends a message with a file attachment.
func (c *Client) SendMessageWithFile(ctx context.Context, channelID string, content string, fileName string, fileData []byte) (*Message, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Add JSON payload.
	if content != "" {
		payloadPart, err := w.CreateFormField("payload_json")
		if err != nil {
			return nil, fmt.Errorf("create payload field: %w", err)
		}
		payload := map[string]string{"content": content}
		if err := json.NewEncoder(payloadPart).Encode(payload); err != nil {
			return nil, fmt.Errorf("encode payload: %w", err)
		}
	}

	// Add file.
	filePart, err := w.CreateFormFile("files[0]", fileName)
	if err != nil {
		return nil, fmt.Errorf("create file part: %w", err)
	}
	if _, err := filePart.Write(fileData); err != nil {
		return nil, fmt.Errorf("write file data: %w", err)
	}
	w.Close()

	respBody, err := c.doRequest(ctx, http.MethodPost, "/channels/"+channelID+"/messages", &buf, w.FormDataContentType())
	if err != nil {
		return nil, fmt.Errorf("uploadFile: %w", err)
	}
	var msg Message
	if err := json.Unmarshal(respBody, &msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	return &msg, nil
}

// TriggerTyping sends a typing indicator to a channel.
func (c *Client) TriggerTyping(ctx context.Context, channelID string) error {
	_, err := c.Call(ctx, http.MethodPost, "/channels/"+channelID+"/typing", nil)
	return err
}

// CreateReaction adds a reaction to a message.
func (c *Client) CreateReaction(ctx context.Context, channelID, messageID, emoji string) error {
	path := fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, emoji)
	_, err := c.Call(ctx, http.MethodPut, path, nil)
	return err
}

// DeleteOwnReaction removes the bot's reaction from a message.
func (c *Client) DeleteOwnReaction(ctx context.Context, channelID, messageID, emoji string) error {
	path := fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, emoji)
	_, err := c.Call(ctx, http.MethodDelete, path, nil)
	return err
}

// CreateThread creates a new thread from a message.
func (c *Client) CreateThread(ctx context.Context, channelID, messageID, name string) (*Channel, error) {
	path := fmt.Sprintf("/channels/%s/messages/%s/threads", channelID, messageID)
	result, err := c.Call(ctx, http.MethodPost, path, map[string]any{
		"name":                 name,
		"auto_archive_duration": 1440, // 24 hours
	})
	if err != nil {
		return nil, fmt.Errorf("createThread: %w", err)
	}
	var ch Channel
	if err := json.Unmarshal(result, &ch); err != nil {
		return nil, fmt.Errorf("decode channel: %w", err)
	}
	return &ch, nil
}

// APIError represents a Discord API error.
type APIError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration // parsed from Retry-After header on 429s
}

func (e *APIError) Error() string {
	return fmt.Sprintf("discord API error %d: %s", e.StatusCode, e.Body)
}

// IsRateLimited returns true if this is a 429 rate limit error.
func (e *APIError) IsRateLimited() bool {
	return e.StatusCode == 429
}
