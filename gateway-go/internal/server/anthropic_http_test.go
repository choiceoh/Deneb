package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestInjectDenebSystem_PreservesOriginal(t *testing.T) {
	s := &Server{}
	existing := llm.SystemString("You are helpful.")
	result := s.injectDenebSystem(existing)
	text := llm.ExtractSystemText(result)
	if !strings.Contains(text, "You are helpful.") {
		t.Errorf("system prompt should contain original text, got: %s", text)
	}
}

func TestHandleMessages_DisabledReturns404(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	s.handleMessages(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestResolveOpenAIModel_Default(t *testing.T) {
	if got := resolveOpenAIModel("claude-sonnet-4-20250514"); got != "claude-sonnet-4-20250514" {
		t.Errorf("got %q", got)
	}
}

func TestResolveOpenAIModel_Override(t *testing.T) {
	t.Setenv("CLAUDENEB_MODEL", "Qwen3.5-35B")
	if got := resolveOpenAIModel("claude-sonnet-4-20250514"); got != "Qwen3.5-35B" {
		t.Errorf("got %q", got)
	}
}
