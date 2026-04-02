package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
)

// newChatCompletionsSrv creates a test server with chat completions enabled.
func newChatCompletionsSrv(t *testing.T) *Server {
	t.Helper()
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	srv.runtimeCfg = &config.GatewayRuntimeConfig{
		OpenAIChatCompletionsEnabled: true,
	}
	srv.chatHandler = nil // Ensure no chat handler for 503 tests.
	return srv
}

func TestChatCompletions_NonStreaming_ValidJSON(t *testing.T) {
	srv := newChatCompletionsSrv(t)
	// chatHandler is nil, so we expect 503.
	body := `{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

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
		t.Errorf("expected type server_error, got %v", errObj["type"])
	}
}

func TestChatCompletions_Streaming_SSEFormat(t *testing.T) {
	srv := newChatCompletionsSrv(t)
	// chatHandler is nil, so we expect 503 before streaming starts.
	body := `{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	// Without chat handler, we still get 503 JSON (before SSE starts).
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestChatCompletions_Auth_401WithoutToken(t *testing.T) {
	srv := newChatCompletionsSrv(t)
	// Enable auth by setting a validator that rejects all tokens.
	srv.authValidator = auth.NewValidator([]byte("test-secret-key-1234567890abcdef"))

	body := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	// No Authorization header.
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

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

func TestChatCompletions_InvalidJSON_400(t *testing.T) {
	srv := newChatCompletionsSrv(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("expected invalid_request_error, got %v", errObj["type"])
	}
}

func TestChatCompletions_EmptyMessages_400(t *testing.T) {
	srv := newChatCompletionsSrv(t)

	body := `{"model":"test","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_NoUserMessage_400(t *testing.T) {
	srv := newChatCompletionsSrv(t)

	body := `{"model":"test","messages":[{"role":"system","content":"you are helpful"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletions_Disabled_404(t *testing.T) {
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	// runtimeCfg is nil, so endpoint should be disabled.

	body := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleChatCompletions(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExtractUserPrompt(t *testing.T) {
	tests := []struct {
		name     string
		messages []OpenAIMessage
		want     string
	}{
		{
			name: "simple string content",
			messages: []OpenAIMessage{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "hello world"},
			},
			want: "hello world",
		},
		{
			name: "last user message wins",
			messages: []OpenAIMessage{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "second"},
			},
			want: "second",
		},
		{
			name:     "no user message",
			messages: []OpenAIMessage{{Role: "system", Content: "sys"}},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUserPrompt(tt.messages)
			if got != tt.want {
				t.Errorf("extractUserPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteSSEData(t *testing.T) {
	w := httptest.NewRecorder()
	flusher := w // httptest.ResponseRecorder implements Flusher.

	writeSSEData(w, flusher, map[string]string{"hello": "world"})

	body := w.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	if !scanner.Scan() {
		t.Fatal("expected at least one line")
	}
	line := scanner.Text()
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("expected SSE data prefix, got: %s", line)
	}
	jsonPart := strings.TrimPrefix(line, "data: ")
	var parsed map[string]string
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		t.Fatalf("SSE data is not valid JSON: %v", err)
	}
	if parsed["hello"] != "world" {
		t.Errorf("expected hello=world, got %v", parsed)
	}
}
