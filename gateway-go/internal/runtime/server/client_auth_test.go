package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func postMiniappClientToken(t *testing.T, s *Server, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/miniapp/rpc", bytes.NewReader(raw))
	req.Header.Set(clientauth.Header, token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiniappRPC(rec, req)
	return rec
}

func TestMiniappRPC_ClientToken(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate client token: %v", err)
	}

	s := newServerWithTelegram(t)
	frame := map[string]any{"id": "1", "method": "miniapp.ping", "params": map[string]any{}}

	// Valid token authenticates: the request gets past auth into dispatch, so it
	// is never 401 (an unknown method yields a dispatch error frame, not 401).
	rec := postMiniappClientToken(t, s, token, frame)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("valid client token should authenticate, got 401: %s", rec.Body.String())
	}

	// Wrong token is rejected with 401.
	rec = postMiniappClientToken(t, s, token+"x", frame)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong client token should be 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid client token") {
		t.Errorf("expected 'invalid client token' error, got %s", rec.Body.String())
	}
}

func TestMiniappRPC_ClientToken_DisabledWhenNoToken(t *testing.T) {
	// No token file → standalone auth disabled → a client-token header is
	// rejected (cannot fall through to initData, which the native client lacks).
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	s := newServerWithTelegram(t)
	frame := map[string]any{"id": "1", "method": "miniapp.ping", "params": map[string]any{}}

	rec := postMiniappClientToken(t, s, "any-token", frame)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("client token with no configured secret should be 401, got %d", rec.Code)
	}
}
