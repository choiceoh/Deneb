package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

func TestNewClientFromDir_LoadsBothFiles(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{
			"client_id":     "test-id.apps.googleusercontent.com",
			"client_secret": "test-secret",
		},
	})
	writeJSON(t, filepath.Join(dir, "calendar_token.json"), map[string]string{
		"access_token":  "ya29.calendar",
		"refresh_token": "1//calendar-refresh",
		"expiry":        time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	c, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}
	if c.clientID != "test-id.apps.googleusercontent.com" {
		t.Errorf("clientID = %q", c.clientID)
	}
	if c.refreshToken != "1//calendar-refresh" {
		t.Errorf("refreshToken = %q", c.refreshToken)
	}
	if c.tokenPath != filepath.Join(dir, "calendar_token.json") {
		t.Errorf("tokenPath = %q", c.tokenPath)
	}
}

func TestNewClientFromDir_MissingTokenFails(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{"client_id": "id", "client_secret": "secret"},
	})
	if _, err := newClientFromDir(dir); err == nil {
		t.Fatal("expected error for missing token file")
	}
}

func TestNewClientFromDir_MissingRefreshTokenFails(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{"client_id": "id", "client_secret": "secret"},
	})
	writeJSON(t, filepath.Join(dir, "calendar_token.json"), map[string]string{
		"access_token": "ya29.no-refresh",
	})
	if _, err := newClientFromDir(dir); err == nil {
		t.Fatal("expected error for token without refresh_token")
	}
}

func TestRefresh_PersistsRotatedToken(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{"client_id": "id", "client_secret": "secret"},
	})
	writeJSON(t, filepath.Join(dir, "calendar_token.json"), map[string]string{
		"access_token":  "ya29.stale",
		"refresh_token": "1//original",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "ya29.fresh",
			"refresh_token": "1//rotated",
			"expires_in": 3600
		}`))
	}))
	defer srv.Close()

	prev := tokenURL
	setTokenURL(srv.URL)
	defer setTokenURL(prev)

	c, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}
	if _, err := c.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if c.accessToken != "ya29.fresh" {
		t.Errorf("accessToken not updated: %q", c.accessToken)
	}
	if c.refreshToken != "1//rotated" {
		t.Errorf("refreshToken not rotated: %q", c.refreshToken)
	}

	// Persisted to disk?
	data, err := os.ReadFile(filepath.Join(dir, "calendar_token.json"))
	if err != nil {
		t.Fatalf("read persisted token: %v", err)
	}
	var got tokenJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode persisted token: %v", err)
	}
	if got.RefreshToken != "1//rotated" {
		t.Errorf("rotated token not persisted: %+v", got)
	}
}
