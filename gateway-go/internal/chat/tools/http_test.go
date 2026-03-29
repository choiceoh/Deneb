package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func callHTTP(t *testing.T, params map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return ToolHTTP()(context.Background(), json.RawMessage(raw))
}

func TestToolHTTP_missingURL(t *testing.T) {
	_, err := callHTTP(t, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestToolHTTP_getRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from server"))
	}))
	defer srv.Close()

	out, err := callHTTP(t, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "HTTP 200") {
		t.Errorf("expected HTTP 200: %q", out)
	}
	if !strings.Contains(out, "hello from server") {
		t.Errorf("expected body: %q", out)
	}
}

func TestToolHTTP_methodDefault(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	callHTTP(t, map[string]any{"url": srv.URL})
	if gotMethod != "GET" {
		t.Errorf("default method should be GET, got %q", gotMethod)
	}
}

func TestToolHTTP_methodPost(t *testing.T) {
	var gotMethod string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	out, err := callHTTP(t, map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"body":   "payload",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("expected POST, got %q", gotMethod)
	}
	if gotBody != "payload" {
		t.Errorf("expected body payload, got %q", gotBody)
	}
	if !strings.Contains(out, "HTTP 201") {
		t.Errorf("expected 201: %q", out)
	}
}

func TestToolHTTP_jsonBody(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	callHTTP(t, map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"json":   map[string]string{"key": "value"},
	})
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("expected json content-type, got %q", gotContentType)
	}
}

func TestToolHTTP_customHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	callHTTP(t, map[string]any{
		"url":     srv.URL,
		"headers": map[string]string{"X-Custom": "test-value"},
	})
	if gotHeader != "test-value" {
		t.Errorf("expected custom header, got %q", gotHeader)
	}
}

func TestToolHTTP_statusHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	out, err := callHTTP(t, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Content-Type: application/json") {
		t.Errorf("expected Content-Type header in output: %q", out)
	}
}

func TestToolHTTP_404response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	out, err := callHTTP(t, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// HTTP 4xx is not a Go error — status is reflected in the output.
	if !strings.Contains(out, "HTTP 404") {
		t.Errorf("expected HTTP 404: %q", out)
	}
}

func TestToolHTTP_truncatesLargeResponse(t *testing.T) {
	large := strings.Repeat("x", 60000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(large))
	}))
	defer srv.Close()

	out, err := callHTTP(t, map[string]any{
		"url":              srv.URL,
		"max_response_chars": 1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker: %q", out[:200])
	}
}

func TestToolHTTP_timeoutCap(t *testing.T) {
	// Verify the timeout parameter is accepted without error (we don't block here).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// timeout above cap (120s) should still work, just be clamped.
	_, err := callHTTP(t, map[string]any{"url": srv.URL, "timeout": 9999.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
