package gmail

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeJSON writes a JSON file to the given path.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNewClientFromDir_InstalledCredentials(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_client.json"), map[string]any{
		"installed": map[string]string{
			"client_id":     "test-id.apps.googleusercontent.com",
			"client_secret": "test-secret",
		},
	})
	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"access_token":  "ya29.test-access",
		"refresh_token": "1//test-refresh",
		"expiry":        time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	client, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}

	if client.clientID != "test-id.apps.googleusercontent.com" {
		t.Errorf("clientID = %q, want test-id.apps.googleusercontent.com", client.clientID)
	}
	if client.clientSecret != "test-secret" {
		t.Errorf("clientSecret = %q, want test-secret", client.clientSecret)
	}
	if client.refreshToken != "1//test-refresh" {
		t.Errorf("refreshToken = %q, want 1//test-refresh", client.refreshToken)
	}
	if client.accessToken != "ya29.test-access" {
		t.Errorf("accessToken = %q, want ya29.test-access", client.accessToken)
	}
}

func TestNewClientFromDir_WebCredentials(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_client.json"), map[string]any{
		"web": map[string]string{
			"client_id":     "web-id.apps.googleusercontent.com",
			"client_secret": "web-secret",
		},
	})
	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"access_token":  "ya29.web",
		"refresh_token": "1//web-refresh",
	})

	client, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}

	if client.clientID != "web-id.apps.googleusercontent.com" {
		t.Errorf("clientID = %q, want web-id", client.clientID)
	}
}

func TestNewClientFromDir_MissingClientFile(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"refresh_token": "1//x",
	})

	_, err := newClientFromDir(dir)
	if err == nil {
		t.Fatal("expected error for missing client file")
	}
}

func TestNewClientFromDir_MissingTokenFile(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_client.json"), map[string]any{
		"installed": map[string]string{
			"client_id":     "id",
			"client_secret": "secret",
		},
	})

	_, err := newClientFromDir(dir)
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
}

func TestNewClientFromDir_EmptyRefreshToken(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_client.json"), map[string]any{
		"installed": map[string]string{
			"client_id":     "id",
			"client_secret": "secret",
		},
	})
	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"access_token":  "ya29.x",
		"refresh_token": "",
	})

	_, err := newClientFromDir(dir)
	if err == nil {
		t.Fatal("expected error for empty refresh token")
	}
}

func TestNewClientFromDir_MissingClientID(t *testing.T) {
	dir := t.TempDir()

	writeJSON(t, filepath.Join(dir, "gmail_client.json"), map[string]any{
		"installed": map[string]string{
			"client_secret": "secret",
		},
	})
	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"refresh_token": "1//x",
	})

	_, err := newClientFromDir(dir)
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

func TestNewClientFromDir_InvalidJSON(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "gmail_client.json"), []byte("{bad json"), 0600)
	writeJSON(t, filepath.Join(dir, "gmail_token.json"), map[string]string{
		"refresh_token": "1//x",
	})

	_, err := newClientFromDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid client JSON")
	}
}

func TestValidToken_UsesCache(t *testing.T) {
	c := &Client{
		accessToken: "cached-token",
		expiry:      time.Now().Add(10 * time.Minute),
	}

	tok, err := c.validToken()
	if err != nil {
		t.Fatalf("validToken: %v", err)
	}
	if tok != "cached-token" {
		t.Errorf("token = %q, want cached-token", tok)
	}
}

func TestValidToken_RefreshesExpired(t *testing.T) {
	// Mock OAuth2 token endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "bad grant_type", 400)
			return
		}
		if r.FormValue("refresh_token") != "1//test-refresh" {
			http.Error(w, "bad refresh_token", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ya29.refreshed",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	tokenPath := filepath.Join(t.TempDir(), "token.json")
	writeJSON(t, tokenPath, map[string]string{"refresh_token": "1//test-refresh"})

	// Save and override tokenURL for test.
	origURL := tokenURL
	defer func() { setTokenURL(origURL) }()
	setTokenURL(srv.URL)

	c := &Client{
		clientID:     "test-id",
		clientSecret: "test-secret",
		accessToken:  "expired",
		refreshToken: "1//test-refresh",
		expiry:       time.Now().Add(-1 * time.Minute), // expired
		tokenPath:    tokenPath,
		httpClient:   &http.Client{},
	}

	tok, err := c.validToken()
	if err != nil {
		t.Fatalf("validToken: %v", err)
	}
	if tok != "ya29.refreshed" {
		t.Errorf("token = %q, want ya29.refreshed", tok)
	}
	if c.accessToken != "ya29.refreshed" {
		t.Errorf("client.accessToken = %q, want ya29.refreshed", c.accessToken)
	}

	// Verify token was persisted.
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	var persisted tokenJSON
	json.Unmarshal(data, &persisted)
	if persisted.AccessToken != "ya29.refreshed" {
		t.Errorf("persisted access_token = %q, want ya29.refreshed", persisted.AccessToken)
	}
}

func TestValidToken_RefreshFailsOnBadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	origURL := tokenURL
	defer func() { setTokenURL(origURL) }()
	setTokenURL(srv.URL)

	c := &Client{
		clientID:     "test-id",
		clientSecret: "test-secret",
		refreshToken: "1//bad",
		expiry:       time.Now().Add(-1 * time.Minute),
		tokenPath:    filepath.Join(t.TempDir(), "token.json"),
		httpClient:   &http.Client{},
	}

	_, err := c.validToken()
	if err == nil {
		t.Fatal("expected error for bad refresh response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want HTTP 401 mention", err)
	}
}

func TestGetClient_RetriableOnFailure(t *testing.T) {
	// Point HOME to empty dir so no real credentials are found.
	t.Setenv("HOME", t.TempDir())

	// Reset the global singleton for this test.
	globalMu.Lock()
	savedClient := globalClient
	globalClient = nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalClient = savedClient
		globalMu.Unlock()
	}()

	// First call should fail (no credentials).
	_, err := DefaultClient()
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}

	// Second call should also try again (not permanently failed like sync.Once).
	_, err2 := DefaultClient()
	if err2 == nil {
		t.Fatal("expected error again for missing credentials")
	}
	// Both should fail, but importantly the second call actually tried (not cached error).
}

func TestPersistToken(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token.json")

	c := &Client{
		accessToken:  "ya29.new",
		refreshToken: "1//refresh",
		expiry:       time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC),
		tokenPath:    tokenPath,
	}

	c.persistToken()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}

	var tok tokenJSON
	if err := json.Unmarshal(data, &tok); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if tok.AccessToken != "ya29.new" {
		t.Errorf("access_token = %q, want ya29.new", tok.AccessToken)
	}
	if tok.RefreshToken != "1//refresh" {
		t.Errorf("refresh_token = %q, want 1//refresh", tok.RefreshToken)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tok.TokenType)
	}

	// Verify file permissions.
	info, _ := os.Stat(tokenPath)
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("token file perm = %o, want 0600", perm)
	}
}
