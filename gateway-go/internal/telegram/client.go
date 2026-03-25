package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const apiBaseURL = "https://api.telegram.org/bot"

// ClientConfig configures the Telegram Bot API client.
type ClientConfig struct {
	// Token is the bot API token.
	Token string
	// HTTPClient overrides the default HTTP client (for proxy support, custom timeouts).
	HTTPClient *http.Client
}

// Client is a thin wrapper around the Telegram Bot API.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Telegram Bot API client.
func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		token:      cfg.Token,
		baseURL:    apiBaseURL + cfg.Token,
		httpClient: httpClient,
	}
}

// Call makes a JSON POST request to the Bot API.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var body io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		body = bytes.NewReader(data)
	}

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

// Upload sends a multipart/form-data request for file uploads.
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

	url := c.baseURL + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

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

// GetMe calls the getMe API method to verify the bot token.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	result, err := c.Call(ctx, "getMe", nil)
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(result, &user); err != nil {
		return nil, fmt.Errorf("decode getMe: %w", err)
	}
	return &user, nil
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

func retryAfterFromParams(p *ResponseParameters) int {
	if p != nil {
		return p.RetryAfter
	}
	return 0
}
