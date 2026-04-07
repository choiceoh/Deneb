package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// httpToolSchema returns the JSON Schema for the http tool.

// toolHTTP implements the http tool for making structured HTTP requests.
func ToolHTTP() ToolFunc {
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

		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range p.Headers {
			req.Header.Set(k, v)
		}

		client := httputil.NewClient(timeout)
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
