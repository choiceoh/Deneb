package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyFCMResponse(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantOK   bool
		wantPrm  bool
		wantAuth bool
	}{
		{"ok", 200, `{"name":"projects/p/messages/1"}`, true, false, false},
		{"unauthorized", 401, `{"error":{"status":"UNAUTHENTICATED"}}`, false, false, true},
		{"forbidden", 403, `{"error":{"status":"PERMISSION_DENIED"}}`, false, false, true},
		{"unregistered 404", 404, `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`, false, true, false},
		{"sender mismatch", 403, `{"error":{"status":"PERMISSION_DENIED","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`, false, false, true},
		{"plain 404", 404, `{}`, false, true, false},
		{"ambiguous 400 not pruned", 400, `{"error":{"status":"INVALID_ARGUMENT"}}`, false, false, false},
		{"server error transient", 503, `{}`, false, false, false},
		{"rate limited transient", 429, `{}`, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := classifyFCMResponse(tc.status, []byte(tc.body))
			if res.OK != tc.wantOK || res.Permanent != tc.wantPrm || res.AuthFailed != tc.wantAuth {
				t.Errorf("status %d: OK=%v Permanent=%v Auth=%v, want OK=%v Permanent=%v Auth=%v",
					tc.status, res.OK, res.Permanent, res.AuthFailed, tc.wantOK, tc.wantPrm, tc.wantAuth)
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

func TestFCMSender_Send_OK(t *testing.T) {
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
	if gotMsg.Message.Notification["title"] != "제목" || gotMsg.Message.Notification["body"] != "본문" {
		t.Errorf("notification = %v", gotMsg.Message.Notification)
	}
	if gotMsg.Message.Data["kind"] != "proactive" {
		t.Errorf("data = %v", gotMsg.Message.Data)
	}
	if gotMsg.Message.Android["priority"] != "high" {
		t.Errorf("android = %v, want priority high", gotMsg.Message.Android)
	}
}

func TestFCMSender_Send_UnregisteredIsPermanent(t *testing.T) {
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

func TestFCMSendEndpoint(t *testing.T) {
	got := fcmSendEndpoint("https://fcm.googleapis.com/", "proj-1")
	want := "https://fcm.googleapis.com/v1/projects/proj-1/messages:send"
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
}
