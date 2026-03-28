package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// httpToolSchema returns the JSON Schema for the http tool.
func httpToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP or HTTPS URL",
			},
			"method": map[string]any{
				"type":        "string",
				"description": "HTTP method",
				"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
				"default":     "GET",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "Request headers as key-value pairs",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Request body as string",
			},
			"json": map[string]any{
				"type":        "object",
				"description": "JSON body (auto-sets Content-Type: application/json)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds",
				"default":     30,
				"minimum":     1,
				"maximum":     120,
			},
			"max_response_chars": map[string]any{
				"type":        "number",
				"description": "Maximum response characters to return",
				"default":     50000,
			},
		},
		"required": []string{"url"},
	}
}

// toolHTTP implements the http tool for making structured HTTP requests.
func toolHTTP() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL             string            `json:"url"`
			Method          string            `json:"method"`
			Headers         map[string]string `json:"headers"`
			Body            string            `json:"body"`
			JSON            json.RawMessage   `json:"json"`
			Timeout         float64           `json:"timeout"`
			MaxResponseChar int               `json:"max_response_chars"`
		}
		if err := jsonutil.UnmarshalInto("http params", input, &p); err != nil {
			return "", err
		}
		if p.URL == "" {
			return "", fmt.Errorf("url is required")
		}

		// Defaults.
		method := strings.ToUpper(p.Method)
		if method == "" {
			method = "GET"
		}
		timeout := 30 * time.Second
		if p.Timeout > 0 {
			timeout = time.Duration(p.Timeout * float64(time.Second))
			if timeout > 120*time.Second {
				timeout = 120 * time.Second
			}
		}
		maxChars := 50000
		if p.MaxResponseChar > 0 {
			maxChars = p.MaxResponseChar
		}

		// Build request body.
		var bodyReader io.Reader
		contentType := ""
		if p.JSON != nil && len(p.JSON) > 0 && string(p.JSON) != "null" {
			bodyReader = bytes.NewReader(p.JSON)
			contentType = "application/json"
		} else if p.Body != "" {
			bodyReader = strings.NewReader(p.Body)
		}

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, method, p.URL, bodyReader)
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}

		req.Header.Set("User-Agent", "Deneb-Gateway/1.0")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range p.Headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		// Read response body with size limit (2x maxChars bytes, capped at 5 MB).
		maxBytes := int64(maxChars * 2)
		if maxBytes > 5*1024*1024 {
			maxBytes = 5 * 1024 * 1024
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		if err != nil {
			return "", fmt.Errorf("read response: %w", err)
		}

		body := string(bodyBytes)
		if len(body) > maxChars {
			body = body[:maxChars] + "\n\n[...truncated]"
		}

		// Format output with status and selected headers.
		var sb strings.Builder
		fmt.Fprintf(&sb, "HTTP %d %s\n", resp.StatusCode, resp.Status)
		for _, h := range []string{"Content-Type", "Content-Length", "Location"} {
			if v := resp.Header.Get(h); v != "" {
				fmt.Fprintf(&sb, "%s: %s\n", h, v)
			}
		}
		sb.WriteString("\n")
		sb.WriteString(body)

		return sb.String(), nil
	}
}
