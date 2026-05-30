package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

const testBotToken = "123456:test-token"

// signFixture builds a Telegram-format initData query string signed with
// botToken. Duplicated from telegram/initdata_test.go because the helper there
// is unexported and we don't want to widen its visibility just for tests.
func signFixture(t *testing.T, botToken string, fields map[string]string) string {
	t.Helper()

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fields[k])
	}

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(botToken))
	secret := secretMAC.Sum(nil)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sb.String()))
	hash := hex.EncodeToString(mac.Sum(nil))

	values := url.Values{}
	for k, v := range fields {
		values.Set(k, v)
	}
	values.Set("hash", hash)
	return values.Encode()
}

func freshInitData(t *testing.T) string {
	t.Helper()
	return signFixture(t, testBotToken, map[string]string{
		"auth_date": strconv.FormatInt(time.Now().UTC().Unix(), 10),
		"query_id":  "AAH-test",
		"user":      `{"id":42,"first_name":"오선택","username":"choiceoh"}`,
	})
}

func newServerWithTelegram(t *testing.T) *Server {
	t.Helper()
	s, err := New(":0")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// Inject a Telegram plugin carrying our test bot token so the
	// authentication path has a key to verify against. The plugin is not
	// started — we only need its Config().BotToken.
	s.telegramPlug = telegram.NewPlugin(&telegram.Config{BotToken: testBotToken}, s.logger)
	return s
}

func postMiniappRPC(t *testing.T, s *Server, authHeader string, body any) *httptest.ResponseRecorder {
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
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
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

func getMiniappAttachment(t *testing.T, s *Server, rawInitData string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{}
	values.Set("initData", rawInitData)
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

func TestHandleMiniappRPC_ValidInitData_Whoami(t *testing.T) {
	s := newServerWithTelegram(t)
	raw := freshInitData(t)

	rec := postMiniappRPC(t, s, "tma "+raw, map[string]any{
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
	if user["firstName"] != "오선택" {
		t.Errorf("firstName = %v, want 오선택", user["firstName"])
	}
}

func TestHandleMiniappGmailAttachment_ValidInitDataStreamsBytes(t *testing.T) {
	s := newServerWithTelegram(t)
	client := &fakeMiniappAttachmentClient{data: []byte("%PDF")}
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return client, nil
	})

	rec := getMiniappAttachment(t, s, freshInitData(t), map[string]string{
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

func TestHandleMiniappGmailAttachment_MissingInitData(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := getMiniappAttachment(t, s, "", map[string]string{
		"messageId":    "m1",
		"attachmentId": "att1",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappGmailAttachment_MissingParams(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := getMiniappAttachment(t, s, freshInitData(t), map[string]string{
		"messageId": "m1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleMiniappGmailAttachment_ClientUnavailable(t *testing.T) {
	s := newServerWithTelegram(t)
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return nil, errors.New("OAuth not configured")
	})

	rec := getMiniappAttachment(t, s, freshInitData(t), map[string]string{
		"messageId":    "m1",
		"attachmentId": "att1",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleMiniappGmailAttachment_GmailNotFound(t *testing.T) {
	s := newServerWithTelegram(t)
	withMiniappAttachmentClientFactory(t, func() (miniappGmailAttachmentClient, error) {
		return &fakeMiniappAttachmentClient{err: errors.New("Gmail API error (HTTP 404): not found")}, nil
	})

	rec := getMiniappAttachment(t, s, freshInitData(t), map[string]string{
		"messageId":    "m1",
		"attachmentId": "att-missing",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleMiniappRPC_MissingAuthHeader(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := postMiniappRPC(t, s, "", map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappRPC_WrongScheme(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := postMiniappRPC(t, s, "Bearer "+freshInitData(t), map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappRPC_TamperedHash(t *testing.T) {
	s := newServerWithTelegram(t)
	raw := freshInitData(t)
	// Replace the last hash character to force a verification failure.
	tampered := raw[:len(raw)-1] + "0"
	if tampered == raw {
		tampered = raw[:len(raw)-1] + "1"
	}

	rec := postMiniappRPC(t, s, "tma "+tampered, map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappRPC_ExpiredInitData(t *testing.T) {
	s := newServerWithTelegram(t)
	// 30 days ago — well past the 24h default TTL.
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Unix()
	raw := signFixture(t, testBotToken, map[string]string{
		"auth_date": strconv.FormatInt(old, 10),
		"user":      `{"id":42,"first_name":"old"}`,
	})

	rec := postMiniappRPC(t, s, "tma "+raw, map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleMiniappRPC_RejectsNonMiniappNamespace(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := postMiniappRPC(t, s, "tma "+freshInitData(t), map[string]any{
		"type": "req", "id": "1", "method": "chat.send",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandleMiniappRPC_NoTelegramPlugin(t *testing.T) {
	s, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	// s.telegramPlug stays nil.
	rec := postMiniappRPC(t, s, "tma "+freshInitData(t), map[string]any{
		"type": "req", "id": "1", "method": "miniapp.ping",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleMiniappRPC_EmptyBody(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := postMiniappRPC(t, s, "tma "+freshInitData(t), nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleMiniappRPC_FrameMissingMethod(t *testing.T) {
	s := newServerWithTelegram(t)
	rec := postMiniappRPC(t, s, "tma "+freshInitData(t), map[string]any{
		"type": "req", "id": "1",
		// method missing
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestExtractMiniappAuthHeader(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		wantRaw string
		wantErr bool
	}{
		{"empty", "", "", true},
		{"single token", "tma", "", true},
		{"lowercase scheme", "tma abc", "abc", false},
		{"mixed case scheme", "TMA abc", "abc", false},
		{"wrong scheme", "Bearer abc", "", true},
		{"empty payload", "tma  ", "", true},
		{"normal", "tma user=42&hash=ff", "user=42&hash=ff", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := extractMiniappAuthHeader(c.header)
			if c.wantErr {
				if err == nil {
					t.Fatalf("got err=nil, want non-nil; raw=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.wantRaw {
				t.Errorf("got %q, want %q", got, c.wantRaw)
			}
		})
	}
}
