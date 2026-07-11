package googleoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func response(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Body: body, Header: make(http.Header)}
}

func TestDoBearerContract(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "context-value")
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Context().Value(ctxKey{}) != "context-value" {
			t.Error("context not propagated")
		}
		if req.Method != http.MethodPost || req.URL.String() != "https://api.example.test/v1/items?q=one" {
			t.Errorf("request = %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body, _ := io.ReadAll(req.Body)
		if string(body) != `{"name":"alpha"}` {
			t.Errorf("body = %q", body)
		}
		return response(http.StatusCreated, io.NopCloser(strings.NewReader(`{"id":1}`))), nil
	})}
	validCalls := 0
	resp, err := DoBearer(ctx, client, func(got context.Context) (string, error) {
		validCalls++
		if got != ctx {
			t.Error("token context changed")
		}
		return "access-token", nil
	}, "https://api.example.test", http.MethodPost, "/v1/items?q=one", strings.NewReader(`{"name":"alpha"}`))
	if err != nil || resp.StatusCode != http.StatusCreated || calls != 1 || validCalls != 1 {
		t.Fatalf("DoBearer = %+v/%v calls=%d/%d", resp, err, calls, validCalls)
	}
	_ = resp.Body.Close()
}

func TestDoBearerNoBodyAndFailureMatrix(t *testing.T) {
	transportCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		transportCalls++
		if req.Body != nil {
			t.Errorf("nil input produced body")
		}
		if got := req.Header.Get("Content-Type"); got != "" {
			t.Errorf("nil body Content-Type = %q", got)
		}
		return response(http.StatusOK, io.NopCloser(strings.NewReader("ok"))), nil
	})}
	resp, err := DoBearer(context.Background(), client, func(context.Context) (string, error) { return "token", nil }, "https://example.test", http.MethodGet, "/", nil)
	if err != nil || transportCalls != 1 {
		t.Fatalf("nil body = %+v/%v", resp, err)
	}
	_ = resp.Body.Close()

	tokenErr := errors.New("token offline")
	if _, err := DoBearer(context.Background(), client, func(context.Context) (string, error) { return "", tokenErr }, "https://example.test", http.MethodGet, "/", nil); !errors.Is(err, tokenErr) {
		t.Fatalf("token error = %v", err)
	}
	if _, err := DoBearer(context.Background(), client, func(context.Context) (string, error) { return " \t", nil }, "https://example.test", http.MethodGet, "/", nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty token error = %v", err)
	}
	if transportCalls != 1 {
		t.Fatalf("transport called after token failure: %d", transportCalls)
	}
	if _, err := DoBearer(context.Background(), client, func(context.Context) (string, error) { return "token", nil }, "://bad-url", http.MethodGet, "/", nil); err == nil {
		t.Fatal("invalid URL accepted")
	}
	transportErr := errors.New("network down")
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, transportErr })}
	if _, err := DoBearer(context.Background(), failing, func(context.Context) (string, error) { return "token", nil }, "https://example.test", http.MethodGet, "/", nil); !errors.Is(err, transportErr) {
		t.Fatalf("transport error = %v", err)
	}
}

func TestJSONRequestAndDecodeContract(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	var gotMethod, gotPath, gotBody string
	request := func(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
		gotMethod, gotPath = method, path
		data, err := io.ReadAll(body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(data)
		return response(http.StatusOK, io.NopCloser(strings.NewReader(`{"name":"result","n":2}`))), nil
	}
	var dest payload
	err := JSON(context.Background(), request, http.MethodPost, "/items", payload{Name: "input", N: 1}, &dest, APIOptions{Service: "Test", MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/items" || gotBody != `{"name":"input","n":1}` {
		t.Fatalf("request = %s %s %s", gotMethod, gotPath, gotBody)
	}
	if dest != (payload{Name: "result", N: 2}) {
		t.Fatalf("dest = %+v", dest)
	}
}

func TestJSONNilPayloadAndNilDestination(t *testing.T) {
	called := false
	closed := false
	request := func(_ context.Context, method, path string, body io.Reader) (*http.Response, error) {
		called = true
		if method != http.MethodDelete || path != "/item/1" || body != nil {
			t.Fatalf("request = %s %s %#v", method, path, body)
		}
		return response(http.StatusOK, closeTracker{Reader: strings.NewReader("not-json but ignored"), closed: &closed}), nil
	}
	if err := JSON(context.Background(), request, http.MethodDelete, "/item/1", nil, nil, APIOptions{Service: "Test", MaxResponseBytes: 100}); err != nil {
		t.Fatal(err)
	}
	if !called || !closed {
		t.Fatalf("called=%v closed=%v", called, closed)
	}
}

type closeTracker struct {
	io.Reader
	closed *bool
}

func (r closeTracker) Close() error { *r.closed = true; return nil }

func TestJSONPayloadMarshalFailurePreventsRequest(t *testing.T) {
	called := false
	err := JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
		called = true
		return nil, nil
	}, http.MethodPost, "/", map[string]any{"channel": make(chan int)}, nil, APIOptions{Service: "Test", MaxResponseBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") || called {
		t.Fatalf("JSON = %v called=%v", err, called)
	}
}

func TestJSONTransportNilAndReadFailures(t *testing.T) {
	want := errors.New("request failed")
	err := JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
		return nil, want
	}, http.MethodGet, "/", nil, nil, APIOptions{Service: "Test", MaxResponseBytes: 100})
	if !errors.Is(err, want) {
		t.Fatalf("request error = %v", err)
	}
	err = JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
		return nil, nil
	}, http.MethodGet, "/", nil, nil, APIOptions{Service: "Calendar", MaxResponseBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "Calendar API returned a nil response") {
		t.Fatalf("nil response error = %v", err)
	}
	readErr := errors.New("read exploded")
	closed := false
	err = JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
		return response(http.StatusOK, closeTracker{Reader: failingReader{readErr}, closed: &closed}), nil
	}, http.MethodGet, "/", nil, nil, APIOptions{Service: "Gmail", MaxResponseBytes: 100})
	if !errors.Is(err, readErr) || !strings.Contains(err.Error(), "Gmail API 응답 읽기 실패") || !closed {
		t.Fatalf("read error = %v closed=%v", err, closed)
	}
}

func TestJSONResponseLimitBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		max  int64
		want string
	}{
		{name: "empty at zero", body: "", max: 0},
		{name: "exact", body: `{"a":1}`, max: 7},
		{name: "one over", body: `{"a":1}`, max: 6, want: "비정상적으로 큼 (>6B)"},
		{name: "nonempty at zero", body: "x", max: 0, want: "비정상적으로 큼 (>0B)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var dest any
			err := JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
				return response(http.StatusOK, io.NopCloser(strings.NewReader(tt.body))), nil
			}, http.MethodGet, "/", nil, &dest, APIOptions{Service: "Test", MaxResponseBytes: tt.max})
			if tt.body == "" {
				// An empty 200 body with a non-nil destination reaches JSON decode.
				if err == nil || !strings.Contains(err.Error(), "unexpected end") {
					t.Fatalf("empty error = %v", err)
				}
				return
			}
			if tt.want == "" && err != nil {
				t.Fatalf("exact error = %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("limit error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestJSONHTTPStatusDefaultAndCustomErrors(t *testing.T) {
	longKorean := strings.Repeat("가", 700)
	for _, tt := range []struct {
		name   string
		status int
		body   string
		custom bool
	}{
		{name: "bad request", status: http.StatusBadRequest, body: "bad request"},
		{name: "unicode long", status: http.StatusInternalServerError, body: longKorean},
		{name: "custom", status: http.StatusTooManyRequests, body: longKorean, custom: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var customStatus int
			var customBody string
			options := APIOptions{Service: "Drive", MaxResponseBytes: 10_000}
			if tt.custom {
				options.StatusError = func(status int, body string) error {
					customStatus, customBody = status, body
					return fmt.Errorf("custom status")
				}
			}
			err := JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
				return response(tt.status, io.NopCloser(strings.NewReader(tt.body))), nil
			}, http.MethodGet, "/", nil, nil, options)
			if err == nil {
				t.Fatal("status error missing")
			}
			if tt.custom {
				if customStatus != tt.status || len([]rune(customBody)) != 503 || !strings.HasSuffix(customBody, "...") || !utf8.ValidString(customBody) {
					t.Fatalf("custom = %d %q", customStatus, customBody)
				}
			} else {
				if !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tt.status)) || !utf8.ValidString(err.Error()) {
					t.Fatalf("default error = %v", err)
				}
			}
		})
	}
}

func TestJSONMalformedDestination(t *testing.T) {
	var dest struct {
		N int `json:"n"`
	}
	err := JSON(context.Background(), func(context.Context, string, string, io.Reader) (*http.Response, error) {
		return response(http.StatusOK, io.NopCloser(strings.NewReader(`{"n":"wrong"}`))), nil
	}, http.MethodGet, "/", nil, &dest, APIOptions{Service: "Test", MaxResponseBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestRefreshRequestEncodingAndResponseContract(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("request = %s %q", req.Method, req.Header.Get("Content-Type"))
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"client_id": "client id", "client_secret": "s&cret", "refresh_token": "refresh+token", "grant_type": "refresh_token"}
		for key, value := range want {
			if got := req.Form.Get(key); got != value {
				t.Errorf("form[%s] = %q, want %q", key, got, value)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-access","refresh_token":"rotated","expires_in":3600,"token_type":"Bearer"}`)
	}))
	defer server.Close()
	before := time.Now()
	got, err := Refresh(context.Background(), server.Client(), RefreshRequest{TokenURL: server.URL, ClientID: "client id", ClientSecret: "s&cret", RefreshToken: "refresh+token"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.AccessToken != "new-access" || got.RefreshToken != "rotated" || !got.Rotated {
		t.Fatalf("Refresh = %+v calls=%d", got, calls)
	}
	if got.Expiry.Before(before.Add(3599*time.Second)) || got.Expiry.After(time.Now().Add(3601*time.Second)) {
		t.Fatalf("expiry = %v", got.Expiry)
	}
}

func TestRefreshFailureMatrixAdditional(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "bad json", status: http.StatusOK, body: "{", want: "토큰 응답 파싱 실패"},
		{name: "missing access", status: http.StatusOK, body: `{"expires_in":3600}`, want: "access_token이 없습니다"},
		{name: "blank access", status: http.StatusOK, body: `{"access_token":"  ","expires_in":3600}`, want: "access_token이 없습니다"},
		{name: "http error", status: http.StatusUnauthorized, body: "denied", want: "HTTP 401"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			_, err := Refresh(context.Background(), srv.Client(), RefreshRequest{TokenURL: srv.URL, RefreshToken: "old"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Refresh error = %v, want %q", err, tt.want)
			}
		})
	}
	if _, err := Refresh(context.Background(), http.DefaultClient, RefreshRequest{TokenURL: "://invalid"}); err == nil || !strings.Contains(err.Error(), "요청 생성 실패") {
		t.Fatalf("invalid URL = %v", err)
	}
	transportErr := errors.New("offline")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, transportErr })}
	if _, err := Refresh(context.Background(), client, RefreshRequest{TokenURL: "https://example.test"}); err == nil || !strings.Contains(err.Error(), "요청 실패") || !errors.Is(err, transportErr) {
		t.Fatalf("transport error = %v", err)
	}
}

func TestRefreshResponseReadAndSizeFailures(t *testing.T) {
	readErr := errors.New("body failed")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, io.NopCloser(failingReader{readErr})), nil
	})}
	if _, err := Refresh(context.Background(), client, RefreshRequest{TokenURL: "https://example.test"}); !errors.Is(err, readErr) || !strings.Contains(err.Error(), "응답 읽기 실패") {
		t.Fatalf("read error = %v", err)
	}
	client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, io.NopCloser(io.LimitReader(zeroReader{}, maxTokenResponseBytes+1))), nil
	})}
	if _, err := Refresh(context.Background(), client, RefreshRequest{TokenURL: "https://example.test"}); err == nil || !strings.Contains(err.Error(), "비정상적으로 큼") {
		t.Fatalf("size error = %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestSourceUsesFreshTokenWithoutNetwork(t *testing.T) {
	transportCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("should not call")
	})}
	expiry := time.Now().Add(2 * time.Hour)
	s := NewSource("Calendar", Loaded{ClientID: "id", ClientSecret: "secret", AccessToken: "fresh", RefreshToken: "refresh", Expiry: expiry}, "/tokens/calendar.json", client)
	got, err := s.ValidToken(context.Background(), "https://token.example.test")
	if err != nil || got != "fresh" || transportCalls != 0 {
		t.Fatalf("ValidToken = %q/%v calls=%d", got, err, transportCalls)
	}
	access, refresh, gotExpiry := s.State()
	if access != "fresh" || refresh != "refresh" || !gotExpiry.Equal(expiry) {
		t.Fatalf("State = %q/%q/%v", access, refresh, gotExpiry)
	}
	if id, secret := s.Credentials(); id != "id" || secret != "secret" {
		t.Fatalf("Credentials = %q/%q", id, secret)
	}
	if s.TokenPath() != "/tokens/calendar.json" {
		t.Fatalf("TokenPath = %q", s.TokenPath())
	}
}

func TestSourceRefreshesPersistsAndSerializesConcurrentCalls(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		_, _ = io.WriteString(w, `{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}`)
	}))
	defer srv.Close()
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	s := NewSource("Gmail", Loaded{ClientID: "id", ClientSecret: "secret", AccessToken: "stale", RefreshToken: "old", Expiry: time.Now().Add(30 * time.Second)}, tokenPath, srv.Client())
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan string, workers)
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := s.ValidToken(context.Background(), srv.URL)
			results <- token
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for token := range results {
		if token != "fresh" {
			t.Errorf("token = %q", token)
		}
	}
	for err := range errs {
		if err != nil {
			t.Errorf("error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d", calls.Load())
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored Token
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "fresh" || stored.RefreshToken != "rotated" || stored.TokenType != "Bearer" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestSourceRefreshFailurePreservesState(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, io.NopCloser(strings.NewReader("denied"))), nil
	})}
	expiry := time.Now().Add(-time.Hour)
	s := NewSource("Test", Loaded{AccessToken: "old-access", RefreshToken: "old-refresh", Expiry: expiry}, filepath.Join(t.TempDir(), "token.json"), client)
	if got, err := s.ValidToken(context.Background(), "https://example.test/token"); got != "" || err == nil {
		t.Fatalf("ValidToken = %q/%v", got, err)
	}
	access, refresh, gotExpiry := s.State()
	if access != "old-access" || refresh != "old-refresh" || !gotExpiry.Equal(expiry) {
		t.Fatalf("state changed = %q/%q/%v", access, refresh, gotExpiry)
	}
}

func TestSourcePersistCurrentState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token.json")
	expiry := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	s := NewSource("Test", Loaded{AccessToken: "access", RefreshToken: "refresh", Expiry: expiry}, path, http.DefaultClient)
	// Missing parent is intentionally logged and ignored; the in-memory token
	// remains usable. Then create it and verify the explicit persistence hook.
	s.Persist()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected file/error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	s.Persist()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Token
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access" || got.RefreshToken != "refresh" || got.Expiry != expiry.Format(time.RFC3339) {
		t.Fatalf("token = %+v", got)
	}
}

func TestLoadCredentialPrecedenceAndExpiryFallback(t *testing.T) {
	dir := t.TempDir()
	clientPath := filepath.Join(dir, "client.json")
	tokenPath := filepath.Join(dir, "token.json")
	writeJSON(t, clientPath, map[string]any{
		"installed": map[string]string{"client_id": "installed-id", "client_secret": "installed-secret"},
		"web":       map[string]string{"client_id": "web-id", "client_secret": "web-secret"},
	})
	writeJSON(t, tokenPath, Token{AccessToken: "access", RefreshToken: "refresh", Expiry: "not-a-date"})
	got, err := Load("Test", clientPath, tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != "installed-id" || got.ClientSecret != "installed-secret" || !got.Expiry.IsZero() {
		t.Fatalf("Loaded = %+v", got)
	}
}

func TestStoredTokenContract(t *testing.T) {
	expiry := time.Date(2027, 1, 2, 3, 4, 5, 6, time.FixedZone("offset", 9*3600))
	got := StoredToken("access", "refresh", expiry)
	want := Token{AccessToken: "access", TokenType: "Bearer", RefreshToken: "refresh", Expiry: expiry.Format(time.RFC3339)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StoredToken = %+v, want %+v", got, want)
	}
}

func TestPersistErrorContract(t *testing.T) {
	cause := errors.New("disk full")
	err := &PersistError{Stage: PersistWrite, TokenPath: "/token", TempPath: "/token.tmp", Err: cause}
	if got := err.Error(); got != "persist OAuth token (write): disk full" {
		t.Fatalf("Error = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap does not expose cause")
	}
}

func TestPersistWriteAndRenameOperationOrdering(t *testing.T) {
	var operations []string
	files := fileOps{
		writeFile: func(path string, data []byte, mode os.FileMode) error {
			operations = append(operations, "write:"+path)
			if mode != 0o600 || !bytes.Contains(data, []byte(`"access_token": "access"`)) {
				t.Fatalf("write mode/data = %o %s", mode, data)
			}
			return nil
		},
		rename: func(old, new string) error {
			operations = append(operations, "rename:"+old+":"+new)
			return nil
		},
		remove: func(string) error { t.Fatal("remove called on success"); return nil },
	}
	if err := persist("/state/token.json", Token{AccessToken: "access"}, files); err != nil {
		t.Fatal(err)
	}
	want := []string{"write:/state/token.json.tmp", "rename:/state/token.json.tmp:/state/token.json"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %v", operations)
	}
}

func TestPersistRenameFailureCleansTempEvenWhenCleanupFails(t *testing.T) {
	renameErr := errors.New("rename failed")
	removeErr := errors.New("cleanup failed")
	removed := ""
	err := persist("/token", Token{AccessToken: "access"}, fileOps{
		writeFile: func(string, []byte, os.FileMode) error { return nil },
		rename:    func(string, string) error { return renameErr },
		remove: func(path string) error {
			removed = path
			return removeErr
		},
	})
	var persistErr *PersistError
	if !errors.As(err, &persistErr) || persistErr.Stage != PersistRename || !errors.Is(err, renameErr) || removed != "/token.tmp" {
		t.Fatalf("persist error = %+v removed=%q", err, removed)
	}
}

func TestTruncateRuneSafeContract(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{name: "short", value: "abc", max: 3, want: "abc"},
		{name: "long ascii", value: "abcd", max: 3, want: "abc..."},
		{name: "unicode exact", value: "가나다", max: 3, want: "가나다"},
		{name: "unicode long", value: "가나다라", max: 3, want: "가나다..."},
		{name: "zero", value: "abc", max: 0, want: "..."},
		{name: "negative", value: "abc", max: -1, want: "..."},
		{name: "empty", value: "", max: 0, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.value, tt.max)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("truncate = %q, want %q", got, tt.want)
			}
		})
	}
}
