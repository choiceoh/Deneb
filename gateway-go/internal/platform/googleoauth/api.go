package googleoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIRequest is the authenticated request seam used by JSON.
type APIRequest func(context.Context, string, string, io.Reader) (*http.Response, error)

// APIOptions controls bounded Google API response decoding.
type APIOptions struct {
	Service          string
	MaxResponseBytes int64
	StatusError      func(statusCode int, body string) error
}

// DoBearer sends an authenticated request using the caller's token source.
func DoBearer(
	ctx context.Context,
	client *http.Client,
	validToken func(context.Context) (string, error),
	baseURL, method, path string,
	body io.Reader,
) (*http.Response, error) {
	token, err := validToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

// JSON sends a GET/POST-style API request and decodes its bounded JSON body.
// A nil payload sends no body; a nil destination accepts an empty response.
func JSON(
	ctx context.Context,
	request APIRequest,
	method, path string,
	payload, dest any,
	options APIOptions,
) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}
	resp, err := request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, options.MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%s API 응답 읽기 실패: %w", options.Service, err)
	}
	if int64(len(responseBody)) > options.MaxResponseBytes {
		return fmt.Errorf("%s API 응답이 비정상적으로 큼 (>%dB)", options.Service, options.MaxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		if options.StatusError != nil {
			return options.StatusError(resp.StatusCode, truncate(string(responseBody), 500))
		}
		return fmt.Errorf("%s API 오류 (HTTP %d): %s", options.Service, resp.StatusCode, truncate(string(responseBody), 500))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(responseBody, dest)
}
