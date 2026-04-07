package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestConnector_Do_BearerAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{
		BaseURL:  ts.URL,
		APIKey:   "test-token",
		AuthMode: "bearer",
	}, nil)

	resp, err := c.Do(context.Background(), http.MethodGet, "/test", nil)
	testutil.NoError(t, err)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestConnector_Do_APIKeyAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key != "sk-123" {
			t.Errorf("expected x-api-key sk-123, got %q", key)
		}
		// Should NOT have Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("unexpected Authorization header: %q", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{
		BaseURL:  ts.URL,
		APIKey:   "sk-123",
		AuthMode: "api_key",
	}, nil)

	resp, err := c.Do(context.Background(), http.MethodGet, "/v1/models", nil)
	testutil.NoError(t, err)
	resp.Body.Close()
}

func TestConnector_Do_CustomHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value-abc" {
			t.Errorf("expected X-Custom=value-abc, got %q", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{
		BaseURL: ts.URL,
		Headers: map[string]string{"X-Custom": "value-abc"},
	}, nil)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	testutil.NoError(t, err)
	resp.Body.Close()
}

func TestConnector_Do_EnvVarExpansion(t *testing.T) {
	os.Setenv("TEST_CONNECTOR_VAR", "expanded-value")
	defer os.Unsetenv("TEST_CONNECTOR_VAR")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "expanded-value" {
			t.Errorf("expected expanded env var, got %q", r.Header.Get("X-Token"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{
		BaseURL: ts.URL,
		Headers: map[string]string{"X-Token": "${TEST_CONNECTOR_VAR}"},
	}, nil)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	testutil.NoError(t, err)
	resp.Body.Close()
}

func TestConnector_JSON_RoundTrip(t *testing.T) {
	type echoResp struct {
		Status string `json:"status"`
		Len    int    `json:"len"`
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(echoResp{Status: "ok", Len: len(body)})
		w.Write(resp)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{BaseURL: ts.URL}, nil)

	var resp echoResp
	err := c.JSON(context.Background(), http.MethodPost, "/echo", map[string]string{"msg": "hello"}, &resp)
	testutil.NoError(t, err)
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %q", resp.Status)
	}
	if resp.Len == 0 {
		t.Error("expected non-zero body length")
	}
}

func TestConnector_JSON_ErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{BaseURL: ts.URL}, nil)

	err := c.JSON(context.Background(), http.MethodGet, "/", nil, nil)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	var ce *ConnectorError
	if !errors.As(err, &ce) {
		t.Fatalf("expected ConnectorError, got %T", err)
	}
	if ce.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", ce.StatusCode)
	}
}

func TestConnector_NoAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("unexpected auth header: %q", auth)
		}
		if key := r.Header.Get("x-api-key"); key != "" {
			t.Errorf("unexpected x-api-key header: %q", key)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewConnector(ConnectorConfig{
		BaseURL:  ts.URL,
		AuthMode: "none",
	}, nil)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	testutil.NoError(t, err)
	resp.Body.Close()
}

func TestExpandEnvVars(t *testing.T) {
	os.Setenv("TEST_EV_A", "hello")
	defer os.Unsetenv("TEST_EV_A")

	tests := []struct {
		input    string
		expected string
	}{
		{"${TEST_EV_A}", "hello"},
		{"prefix-${TEST_EV_A}-suffix", "prefix-hello-suffix"},
		{"no-vars-here", "no-vars-here"},
		{"${NONEXISTENT_VAR_XYZ}", ""},
	}

	for _, tt := range tests {
		result := ExpandEnvVars(tt.input)
		if result != tt.expected {
			t.Errorf("ExpandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
