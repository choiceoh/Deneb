package nativeapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// TestHandleMiniappChatStream_RejectsOversizeBody verifies the MaxBytesReader cap
// rejects an over-limit body with 413 before any agent work (the same mechanism
// guards handleMiniappRPC). One byte over the cap is enough to trip it.
func TestHandleMiniappChatStream_RejectsOversizeBody(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{})

	body := make([]byte, maxMiniappChatStreamBodyBytes+1)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/v1/miniapp/chat/stream", bytes.NewReader(body))
	req.Header.Set(clientauth.Header, token)
	rec := httptest.NewRecorder()

	s.ChatStream(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (request entity too large)", rec.Code)
	}
}

// TestHandleMiniappChatStream_AllowsNormalBody is the negative control: a small
// body passes the cap (and then fails later for an unrelated reason — missing
// message / no chat handler — never with 413).
func TestHandleMiniappChatStream_AllowsNormalBody(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/v1/miniapp/chat/stream", bytes.NewReader([]byte(`{"message":""}`)))
	req.Header.Set(clientauth.Header, token)
	rec := httptest.NewRecorder()

	s.ChatStream(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("a small body must not be rejected as too large")
	}
}
