package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
)

// newResponsesSrv creates a test server with responses endpoint enabled.
func newResponsesSrv(t *testing.T) *Server {
	t.Helper()
	srv := New(":0")
	srv.runtimeCfg = &config.GatewayRuntimeConfig{
		OpenResponsesEnabled: true,
	}
	srv.chatHandler = nil // Ensure no chat handler for 503 tests.
	return srv
}

func TestResponses_NonStreaming_ValidJSON(t *testing.T) {
	srv := newResponsesSrv(t)
	// chatHandler is nil, so we expect 503.
	body := `{"model":"test-model","input":"hello world"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no chat handler), got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["type"] != "server_error" {
		t.Errorf("expected server_error, got %v", errObj["type"])
	}
}

func TestResponses_Auth_401(t *testing.T) {
	srv := newResponsesSrv(t)
	srv.authValidator = auth.NewValidator([]byte("test-secret-key-1234567890abcdef"))

	body := `{"model":"test","input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["type"] != "authentication_error" {
		t.Errorf("expected authentication_error, got %v", errObj["type"])
	}
}

func TestResponses_StringInput(t *testing.T) {
	srv := newResponsesSrv(t)
	body := `{"model":"test","input":"direct string input"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	// Without chat handler, expect 503 (proves we got past input parsing).
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestResponses_ArrayInput(t *testing.T) {
	srv := newResponsesSrv(t)
	body := `{
		"model":"test",
		"input":[
			{"role":"user","content":"first message"},
			{"role":"user","content":"second message"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	// Without chat handler, expect 503 (proves we got past input parsing).
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestResponses_EmptyInput_400(t *testing.T) {
	srv := newResponsesSrv(t)
	body := `{"model":"test","input":""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResponses_InvalidJSON_400(t *testing.T) {
	srv := newResponsesSrv(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{broken"))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResponses_Disabled_404(t *testing.T) {
	srv := New(":0")
	body := `{"model":"test","input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleResponses(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExtractResponsesInput(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "string input",
			input: "hello world",
			want:  "hello world",
		},
		{
			name: "array with string content",
			input: []any{
				map[string]any{"role": "user", "content": "msg1"},
				map[string]any{"role": "user", "content": "msg2"},
			},
			want: "msg1\nmsg2",
		},
		{
			name: "array with content blocks",
			input: []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_text", "text": "block1"},
						map[string]any{"type": "input_text", "text": "block2"},
					},
				},
			},
			want: "block1\nblock2",
		},
		{
			name: "filters non-user roles",
			input: []any{
				map[string]any{"role": "system", "content": "instructions"},
				map[string]any{"role": "user", "content": "user msg"},
			},
			want: "user msg",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractResponsesInput(tt.input)
			if got != tt.want {
				t.Errorf("extractResponsesInput() = %q, want %q", got, tt.want)
			}
		})
	}
}
