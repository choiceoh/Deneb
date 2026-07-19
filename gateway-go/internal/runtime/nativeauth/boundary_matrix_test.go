package nativeauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func nativeAuthTestToken(t *testing.T) string {
	t.Helper()
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return token
}

func TestAuthenticateHeaderRejectionBoundaryMatrix(t *testing.T) {
	_ = nativeAuthTestToken(t)
	tests := []struct{ name, token, wantError string }{
		{
			name:      "token 0 \"\"",
			token:     "",
			wantError: "missing client token",
		},
		{
			name:      "token 1 \" \"",
			token:     " ",
			wantError: "missing client token",
		},
		{
			name:      "token 2 \"\\t\"",
			token:     "\t",
			wantError: "missing client token",
		},
		{
			name:      "token 3 \"\\n\"",
			token:     "\n",
			wantError: "missing client token",
		},
		{
			name:      "token 4 \"wrong\"",
			token:     "wrong",
			wantError: "invalid client token",
		},
		{
			name:      "token 5 \"WRONG\"",
			token:     "WRONG",
			wantError: "invalid client token",
		},
		{
			name:      "token 6 \"0\"",
			token:     "0",
			wantError: "invalid client token",
		},
		{
			name:      "token 7 \"null\"",
			token:     "null",
			wantError: "invalid client token",
		},
		{
			name:      "token 8 \"undefined\"",
			token:     "undefined",
			wantError: "invalid client token",
		},
		{
			name:      "token 9 \"Bearer token\"",
			token:     "Bearer token",
			wantError: "invalid client token",
		},
		{
			name:      "token 10 \"token \"",
			token:     "token ",
			wantError: "invalid client token",
		},
		{
			name:      "token 11 \" token\"",
			token:     " token",
			wantError: "invalid client token",
		},
		{
			name:      "token 12 \"a.b.c\"",
			token:     "a.b.c",
			wantError: "invalid client token",
		},
		{
			name:      "token 13 \"🚀\"",
			token:     "🚀",
			wantError: "invalid client token",
		},
		{
			name:      "token 14 \"x\"",
			token:     "x",
			wantError: "invalid client token",
		},
		{
			name:      "token 15 \"xxxxxxxxxxxxxxxx\"",
			token:     "xxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "token 16 \"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			token:     "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "token 17 \"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			token:     "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "token 18 \"${TOKEN}\"",
			token:     "${TOKEN}",
			wantError: "invalid client token",
		},
		{
			name:      "token 19 \"%00\"",
			token:     "%00",
			wantError: "invalid client token",
		},
		{
			name:      "token 20 \"../token\"",
			token:     "../token",
			wantError: "invalid client token",
		},
		{
			name:      "token 21 \"' OR 1=1 --\"",
			token:     "' OR 1=1 --",
			wantError: "invalid client token",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/rpc", nil)
			if tc.token != "" {
				req.Header.Set(clientauth.Header, tc.token)
			}
			rec := httptest.NewRecorder()
			identity, ok := Authenticate(rec, req, nil)
			if ok || identity != nil {
				t.Fatalf("identity=%#v ok=%v", identity, ok)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status=%d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Errorf("body=%q want=%q", rec.Body.String(), tc.wantError)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
			}
			if rec.Header().Get("Server") != "deneb-gateway" {
				t.Errorf("Server=%q", rec.Header().Get("Server"))
			}
		})
	}
}

func TestAuthenticateDownloadRejectionBoundaryMatrix(t *testing.T) {
	_ = nativeAuthTestToken(t)
	tests := []struct{ name, token, wantError string }{
		{
			name:      "query token 0",
			token:     "",
			wantError: "missing client token",
		},
		{
			name:      "query token 1",
			token:     " ",
			wantError: "missing client token",
		},
		{
			name:      "query token 2",
			token:     "\t",
			wantError: "missing client token",
		},
		{
			name:      "query token 3",
			token:     "\n",
			wantError: "missing client token",
		},
		{
			name:      "query token 4",
			token:     "wrong",
			wantError: "invalid client token",
		},
		{
			name:      "query token 5",
			token:     "WRONG",
			wantError: "invalid client token",
		},
		{
			name:      "query token 6",
			token:     "0",
			wantError: "invalid client token",
		},
		{
			name:      "query token 7",
			token:     "null",
			wantError: "invalid client token",
		},
		{
			name:      "query token 8",
			token:     "undefined",
			wantError: "invalid client token",
		},
		{
			name:      "query token 9",
			token:     "Bearer token",
			wantError: "invalid client token",
		},
		{
			name:      "query token 10",
			token:     "token ",
			wantError: "invalid client token",
		},
		{
			name:      "query token 11",
			token:     " token",
			wantError: "invalid client token",
		},
		{
			name:      "query token 12",
			token:     "a.b.c",
			wantError: "invalid client token",
		},
		{
			name:      "query token 13",
			token:     "🚀",
			wantError: "invalid client token",
		},
		{
			name:      "query token 14",
			token:     "x",
			wantError: "invalid client token",
		},
		{
			name:      "query token 15",
			token:     "xxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "query token 16",
			token:     "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "query token 17",
			token:     "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantError: "invalid client token",
		},
		{
			name:      "query token 18",
			token:     "${TOKEN}",
			wantError: "invalid client token",
		},
		{
			name:      "query token 19",
			token:     "%00",
			wantError: "invalid client token",
		},
		{
			name:      "query token 20",
			token:     "../token",
			wantError: "invalid client token",
		},
		{
			name:      "query token 21",
			token:     "' OR 1=1 --",
			wantError: "invalid client token",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := "/download"
			if tc.token != "" {
				u += "?clientToken=" + url.QueryEscape(tc.token)
			}
			rec := httptest.NewRecorder()
			identity, ok := AuthenticateDownload(rec, httptest.NewRequest(http.MethodGet, u, nil), nil)
			if ok || identity != nil {
				t.Fatalf("identity=%#v ok=%v", identity, ok)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status=%d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Errorf("body=%q want=%q", rec.Body.String(), tc.wantError)
			}
		})
	}
}

func TestAuthenticateTrimsWhitespaceAndIgnoresCrossChannelToken(t *testing.T) {
	token := nativeAuthTestToken(t)
	for _, tc := range []struct{ name, supplied string }{
		{name: "exact", supplied: token},
		{name: "leading space", supplied: "  " + token},
		{name: "trailing space", supplied: token + "  "},
		{name: "tabs", supplied: "\t" + token + "\t"},
		{name: "newlines", supplied: "\n" + token + "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/rpc", nil)
			req.Header.Set(clientauth.Header, tc.supplied)
			rec := httptest.NewRecorder()
			identity, ok := Authenticate(rec, req, nil)
			if !ok || identity == nil || identity.User == nil {
				t.Fatalf("identity=%#v ok=%v body=%q", identity, ok, rec.Body.String())
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status=%d", rec.Code)
			}
		})
	}
	t.Run("header ignores valid query token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/rpc?clientToken="+url.QueryEscape(token), nil)
		rec := httptest.NewRecorder()
		if identity, ok := Authenticate(rec, req, nil); ok || identity != nil {
			t.Fatal("header auth accepted query-only token")
		}
	})
	t.Run("download ignores valid header token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/download", nil)
		req.Header.Set(clientauth.Header, token)
		rec := httptest.NewRecorder()
		if identity, ok := AuthenticateDownload(rec, req, nil); ok || identity != nil {
			t.Fatal("download auth accepted header-only token")
		}
	})
}

func TestSyntheticOperatorIdentityBoundary(t *testing.T) {
	before := time.Now()
	identity := syntheticOperatorIdentity()
	after := time.Now()
	if identity == nil || identity.User == nil {
		t.Fatalf("identity=%#v", identity)
	}
	if identity.User.ID != nativeClientUserID {
		t.Errorf("user id=%d want=%d", identity.User.ID, nativeClientUserID)
	}
	if identity.User.FirstName != "Deneb Native Client" {
		t.Errorf("first name=%q", identity.User.FirstName)
	}
	if identity.ChatType != "private" {
		t.Errorf("chat type=%q", identity.ChatType)
	}
	if identity.AuthDate.Before(before) || identity.AuthDate.After(after) {
		t.Errorf("auth date=%s outside [%s,%s]", identity.AuthDate, before, after)
	}
	if identity.Raw["auth"] != "client_token" {
		t.Errorf("raw auth=%q", identity.Raw["auth"])
	}
	if len(identity.Raw) != 1 {
		t.Errorf("raw=%v", identity.Raw)
	}
	second := syntheticOperatorIdentity()
	identity.User.FirstName = "mutated"
	identity.Raw["auth"] = "mutated"
	if second.User.FirstName != "Deneb Native Client" {
		t.Error("identity user storage shared")
	}
	if second.Raw["auth"] != "client_token" {
		t.Error("identity raw map shared")
	}
}

func TestWriteJSONBoundaryMatrix(t *testing.T) {
	tests := []struct {
		name   string
		status int
		value  any
		want   string
	}{
		{name: "ok map", status: 200, value: map[string]any{"ok": true}, want: `{"ok":true}`},
		{name: "created string", status: 201, value: "created", want: `"created"`},
		{name: "accepted nil", status: 202, value: nil, want: `null`},
		{name: "bad request", status: 400, value: map[string]string{"error": "bad"}, want: `{"error":"bad"}`},
		{name: "unauthorized", status: 401, value: []string{"missing"}, want: `["missing"]`},
		{name: "forbidden", status: 403, value: 42, want: `42`},
		{name: "server error", status: 500, value: false, want: `false`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeJSON(rec, tc.status, tc.value, nil)
			if rec.Code != tc.status {
				t.Errorf("status=%d want=%d", rec.Code, tc.status)
			}
			if strings.TrimSpace(rec.Body.String()) != tc.want {
				t.Errorf("body=%q want=%q", rec.Body.String(), tc.want)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
			}
			if rec.Header().Get("Server") != "deneb-gateway" {
				t.Errorf("Server=%q", rec.Header().Get("Server"))
			}
		})
	}
}

func TestWriteJSONEncodeFailureIsContained(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]any{"bad": make(chan int)}, logger)
	if rec.Code != http.StatusTeapot {
		t.Errorf("status=%d", rec.Code)
	}
	if !strings.Contains(logs.String(), "json encode error") {
		t.Errorf("logs=%q", logs.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
	}
	writeJSON(httptest.NewRecorder(), http.StatusOK, map[string]any{"bad": make(chan int)}, nil)
}

func TestNativeAuthenticationConcurrent(t *testing.T) {
	token := nativeAuthTestToken(t)
	const workers = 128
	const iterations = 50
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/rpc?w=%d&i=%d", worker, i), nil)
				req.Header.Set(clientauth.Header, token)
				identity, ok := Authenticate(httptest.NewRecorder(), req, nil)
				if !ok || identity == nil || identity.User == nil || identity.User.ID != nativeClientUserID {
					errs <- fmt.Errorf("header worker=%d i=%d identity=%#v ok=%v", worker, i, identity, ok)
					return
				}
				download := httptest.NewRequest(http.MethodGet, "/download?clientToken="+url.QueryEscape(token), nil)
				identity, ok = AuthenticateDownload(httptest.NewRecorder(), download, nil)
				if !ok || identity == nil || identity.User == nil {
					errs <- fmt.Errorf("download worker=%d i=%d", worker, i)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestNativeAuthJSONResponsesDecode(t *testing.T) {
	_ = nativeAuthTestToken(t)
	rec := httptest.NewRecorder()
	_, _ = Authenticate(rec, httptest.NewRequest(http.MethodPost, "/rpc", nil), nil)
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%q", err, rec.Body.String())
	}
	if payload["error"] != "missing client token" {
		t.Errorf("payload=%v", payload)
	}
}
