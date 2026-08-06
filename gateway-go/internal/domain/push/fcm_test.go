package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyFCMResponseParsesStatusIntoOutcome(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantOK   bool
		wantPrm  bool
		wantAuth bool
		wantTmp  bool
	}{
		{"ok", 200, `{"name":"projects/p/messages/1"}`, true, false, false, false},
		{"unauthorized", 401, `{"error":{"status":"UNAUTHENTICATED"}}`, false, false, true, false},
		{"forbidden", 403, `{"error":{"status":"PERMISSION_DENIED"}}`, false, false, true, false},
		{"unregistered 404", 404, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`, false, true, false, false},
		{"sender mismatch", 403, `{"error":{"status":"PERMISSION_DENIED","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`, false, false, true, false},
		{"plain 404", 404, `{}`, false, true, false, false},
		{"ambiguous 400 not pruned", 400, `{"error":{"status":"INVALID_ARGUMENT"}}`, false, false, false, false},
		{"server error transient", 503, `{}`, false, false, false, true},
		{"rate limited transient", 429, `{}`, false, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := classifyFCMResponse(tc.status, []byte(tc.body))
			if res.OK != tc.wantOK || res.Permanent != tc.wantPrm || res.AuthFailed != tc.wantAuth || res.Transient != tc.wantTmp {
				t.Errorf("status %d: OK=%v Permanent=%v Auth=%v Transient=%v, want OK=%v Permanent=%v Auth=%v Transient=%v",
					tc.status, res.OK, res.Permanent, res.AuthFailed, res.Transient,
					tc.wantOK, tc.wantPrm, tc.wantAuth, tc.wantTmp)
			}
		})
	}
}

// newTestSender builds an FCMSender wired to two test servers: tokenSrv mints
// the access token, fcmSrv answers the messages:send call.
func newTestSender(t *testing.T, tokenSrv, fcmSrv *httptest.Server) *FCMSender {
	t.Helper()
	raw, _ := testCredentials(t, tokenSrv.URL)
	sa, err := parseServiceAccount(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &FCMSender{
		ts:        newTokenSource(sa, tokenSrv.Client()),
		http:      fcmSrv.Client(),
		projectID: sa.projectID,
		baseURL:   fcmSrv.URL,
	}
}

func tokenServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok-xyz","expires_in":3600}`))
	}))
}

func TestFCMSenderSendWritesDataOnlyMessageWithAuthHeader(t *testing.T) {
	tokenSrv := tokenServer(t)
	defer tokenSrv.Close()

	var gotAuth, gotPath string
	var gotMsg struct {
		Message struct {
			Token        string            `json:"token"`
			Notification map[string]string `json:"notification"`
			Data         map[string]string `json:"data"`
			Android      map[string]any    `json:"android"`
		} `json:"message"`
	}
	fcmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotMsg)
		_, _ = w.Write([]byte(`{"name":"projects/deneb-test/messages/1"}`))
	}))
	defer fcmSrv.Close()

	s := newTestSender(t, tokenSrv, fcmSrv)
	res := s.Send(context.Background(), "device-1", "제목", "본문", map[string]string{"kind": "proactive"})
	if !res.OK || res.Err != nil {
		t.Fatalf("send: %+v", res)
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization = %q, want Bearer tok-xyz", gotAuth)
	}
	if gotPath != "/v1/projects/deneb-test/messages:send" {
		t.Errorf("path = %q", gotPath)
	}
	if gotMsg.Message.Token != "device-1" {
		t.Errorf("token = %q", gotMsg.Message.Token)
	}
	// Data-only contract: no `notification` block (the app must build the
	// MessagingStyle notification itself for Android Auto), title/body in data.
	if len(gotMsg.Message.Notification) != 0 {
		t.Errorf("notification block must be absent, got %v", gotMsg.Message.Notification)
	}
	if gotMsg.Message.Data["title"] != "제목" || gotMsg.Message.Data["body"] != "본문" {
		t.Errorf("data title/body = %v", gotMsg.Message.Data)
	}
	if gotMsg.Message.Data["kind"] != "proactive" {
		t.Errorf("data = %v", gotMsg.Message.Data)
	}
	if gotMsg.Message.Android["priority"] != "high" {
		t.Errorf("android = %v, want priority high", gotMsg.Message.Android)
	}
}

// Send must stay data-only even with a nil caller data map, and explicit
// title/body must win over colliding caller-supplied keys.
func TestFCMSenderSendStaysDataOnlyWithNilOrCollidingData(t *testing.T) {
	tokenSrv := tokenServer(t)
	defer tokenSrv.Close()

	var gotRaw map[string]any
	var gotMsg struct {
		Message struct {
			Data map[string]string `json:"data"`
		} `json:"message"`
	}
	fcmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotRaw)
		_ = json.Unmarshal(body, &gotMsg)
		_, _ = w.Write([]byte(`{"name":"projects/deneb-test/messages/2"}`))
	}))
	defer fcmSrv.Close()

	s := newTestSender(t, tokenSrv, fcmSrv)
	if res := s.Send(context.Background(), "device-1", "t1", "b1", nil); !res.OK {
		t.Fatalf("send nil data: %+v", res)
	}
	msg, _ := gotRaw["message"].(map[string]any)
	if _, has := msg["notification"]; has {
		t.Errorf("nil-data send still carries a notification block: %v", msg)
	}
	if gotMsg.Message.Data["title"] != "t1" || gotMsg.Message.Data["body"] != "b1" {
		t.Errorf("data = %v, want title/body present", gotMsg.Message.Data)
	}

	if res := s.Send(context.Background(), "device-1", "real-title", "real-body",
		map[string]string{"title": "spoof", "kind": "proactive"}); !res.OK {
		t.Fatalf("send colliding data: %+v", res)
	}
	if gotMsg.Message.Data["title"] != "real-title" || gotMsg.Message.Data["kind"] != "proactive" {
		t.Errorf("data = %v, want explicit title to win and kind preserved", gotMsg.Message.Data)
	}
}

func TestFCMSenderSendClassifiesUnregisteredAsPermanentFailure(t *testing.T) {
	tokenSrv := tokenServer(t)
	defer tokenSrv.Close()
	fcmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
	}))
	defer fcmSrv.Close()

	s := newTestSender(t, tokenSrv, fcmSrv)
	res := s.Send(context.Background(), "stale-token", "t", "b", nil)
	if !res.Permanent {
		t.Fatalf("want Permanent, got %+v", res)
	}
	if res.Err != nil && strings.Contains(res.Err.Error(), "stale-token") {
		t.Errorf("error leaks device token: %v", res.Err)
	}
}

func TestFCMSender_Send_AuthFailure(t *testing.T) {
	tokenSrv := tokenServer(t)
	defer tokenSrv.Close()
	fcmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"status":"UNAUTHENTICATED"}}`))
	}))
	defer fcmSrv.Close()

	s := newTestSender(t, tokenSrv, fcmSrv)
	res := s.Send(context.Background(), "device-1", "t", "b", nil)
	if !res.AuthFailed {
		t.Fatalf("want AuthFailed, got %+v", res)
	}
}

func TestFCMSenderSendClassifiesTokenEndpointFailures(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		wantAuth      bool
		wantTransient bool
	}{
		{"auth", http.StatusBadRequest, true, false},
		{"rate limit", http.StatusTooManyRequests, false, true},
		{"service unavailable", http.StatusServiceUnavailable, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"do-not-log"}`))
			}))
			defer tokenSrv.Close()
			fcmSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("FCM send endpoint should not be called when token minting fails")
			}))
			defer fcmSrv.Close()

			s := newTestSender(t, tokenSrv, fcmSrv)
			res := s.Send(context.Background(), "device-1", "t", "b", nil)
			if res.AuthFailed != tc.wantAuth || res.Transient != tc.wantTransient {
				t.Fatalf("AuthFailed=%v Transient=%v, want AuthFailed=%v Transient=%v",
					res.AuthFailed, res.Transient, tc.wantAuth, tc.wantTransient)
			}
			if res.Err == nil {
				t.Fatal("expected classified error")
			}
			if strings.Contains(res.Err.Error(), "do-not-log") {
				t.Fatalf("token response body leaked in error: %v", res.Err)
			}
		})
	}
}

func TestFCMSendEndpointFormatsProjectURL(t *testing.T) {
	got := fcmSendEndpoint("https://fcm.googleapis.com/", "proj-1")
	want := "https://fcm.googleapis.com/v1/projects/proj-1/messages:send"
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
}
