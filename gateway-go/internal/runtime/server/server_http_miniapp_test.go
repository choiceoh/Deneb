package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// newTestServer builds a minimal server for tests.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(":0")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

// withClientToken enables standalone client-token auth backed by a temp state
// dir and returns the freshly generated secret.
func withClientToken(t *testing.T) string {
	t.Helper()
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate client token: %v", err)
	}
	return token
}

func postMiniappRPC(t *testing.T, s *Server, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = raw
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/miniapp/rpc", bytes.NewReader(buf))
	if token != "" {
		req.Header.Set(clientauth.Header, token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiniappRPC(rec, req)
	return rec
}

type fakeMiniappAttachmentClient struct {
	data          []byte
	err           error
	seenMessageID string
	seenAttachID  string
}

func (f *fakeMiniappAttachmentClient) GetAttachment(_ context.Context, messageID, attachmentID string) ([]byte, error) {
	f.seenMessageID = messageID
	f.seenAttachID = attachmentID
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

func getMiniappAttachment(t *testing.T, s *Server, token string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{}
	if token != "" {
		values.Set("clientToken", token)
	}
	for k, v := range params {
		values.Set(k, v)
	}
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/api/v1/miniapp/gmail/attachment?"+values.Encode(),
		nil,
	)
	rec := httptest.NewRecorder()
	s.handleMiniappGmailAttachment(rec, req)
	return rec
}

func withMiniappAttachmentClientFactory(t *testing.T, factory func() (miniappGmailAttachmentClient, error)) {
	t.Helper()
	orig := miniappGmailAttachmentClientFactory
	miniappGmailAttachmentClientFactory = factory
	t.Cleanup(func() { miniappGmailAttachmentClientFactory = orig })
}

func TestHandleMiniappRPC_ValidClientToken_Whoami(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)

	rec := postMiniappRPC(t, s, token, map[string]any{
		"type":   "req",
		"id":     "1",
		"method": "miniapp.whoami",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		OK      bool            `json:"ok"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.OK {
		t.Fatalf("response not OK: %s", rec.Body.String())
	}
	var user map[string]any
	if err := json.Unmarshal(got.Payload, &user); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	// Native-client sessions carry the synthetic operator identity.
	if user["firstName"] != "Deneb Native Client" {
		t.Errorf("firstName = %v, want Deneb Native Client", user["firstName"])
	}
}

func TestHandleMiniappGmailAttachment_ValidClientTokenStreamsBytes(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	client := &fakeMiniappAttachmentClient{data: []byte("%PDF")}
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return client, nil
	})

	rec := getMiniappAttachment(t, s, token, map[string]string{
		"messageId":    "m1",
		"attachmentId": "att1",
		"filename":     "report.pdf",
		"mimeType":     "application/pdf",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "%PDF" {
		t.Errorf("body = %q, want %%PDF", rec.Body.String())
	}
	if client.seenMessageID != "m1" || client.seenAttachID != "att1" {
		t.Errorf("GetAttachment args = %q/%q, want m1/att1", client.seenMessageID, client.seenAttachID)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "report.pdf") {
		t.Errorf("Content-Disposition = %q, want filename", got)
	}
}

func TestHandleMiniappGmailAttachment_MissingToken(t *testing.T) {
	withClientToken(t) // enable standalone auth, but send no token
	s := newTestServer(t)
	rec := getMiniappAttachment(t, s, "", map[string]string{
		"messageId":    "m1",
		"attachmentId": "att1",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappGmailAttachment_MissingParams(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	rec := getMiniappAttachment(t, s, token, map[string]string{
		"messageId": "m1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleMiniappGmailAttachment_ClientUnavailable(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return nil, errors.New("OAuth not configured")
	})

	rec := getMiniappAttachment(t, s, token, map[string]string{
		"messageId":    "m1",
		"attachmentId": "att1",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleMiniappGmailAttachment_GmailNotFound(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return &fakeMiniappAttachmentClient{err: errors.New("Gmail API error (HTTP 404): not found")}, nil
	})

	rec := getMiniappAttachment(t, s, token, map[string]string{
		"messageId":    "m1",
		"attachmentId": "att-missing",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleMiniappRPC_MissingToken(t *testing.T) {
	withClientToken(t) // enable standalone auth, but send no token
	s := newTestServer(t)
	rec := postMiniappRPC(t, s, "", map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappRPC_InvalidToken(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	rec := postMiniappRPC(t, s, token+"x", map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "invalid client token") {
		t.Errorf("expected 'invalid client token' error, got %s", rec.Body.String())
	}
}

func TestHandleMiniappRPC_RejectsNonMiniappNamespace(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	rec := postMiniappRPC(t, s, token, map[string]any{
		"type": "req", "id": "1", "method": "chat.send",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandleMiniappRPC_EmptyBody(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	rec := postMiniappRPC(t, s, token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleMiniappRPC_FrameMissingMethod(t *testing.T) {
	token := withClientToken(t)
	s := newTestServer(t)
	rec := postMiniappRPC(t, s, token, map[string]any{
		"type": "req", "id": "1",
		// method missing
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
