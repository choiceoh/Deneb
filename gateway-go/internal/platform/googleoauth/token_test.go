package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadSupportsInstalledAndWebCredentials(t *testing.T) {
	expiry := time.Date(2026, 7, 11, 12, 30, 0, 0, time.UTC)
	for _, credentialKind := range []string{"installed", "web"} {
		t.Run(credentialKind, func(t *testing.T) {
			dir := t.TempDir()
			clientPath := filepath.Join(dir, "client.json")
			tokenPath := filepath.Join(dir, "token.json")
			writeJSON(t, clientPath, map[string]any{
				credentialKind: map[string]string{
					"client_id":     "client-id",
					"client_secret": "client-secret",
				},
			})
			writeJSON(t, tokenPath, Token{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				Expiry:       expiry.Format(time.RFC3339),
			})

			loaded, err := Load("Test", clientPath, tokenPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if loaded.ClientID != "client-id" || loaded.ClientSecret != "client-secret" {
				t.Errorf("credentials = %q/%q", loaded.ClientID, loaded.ClientSecret)
			}
			if loaded.AccessToken != "access-token" || loaded.RefreshToken != "refresh-token" {
				t.Errorf("tokens = %q/%q", loaded.AccessToken, loaded.RefreshToken)
			}
			if !loaded.Expiry.Equal(expiry) {
				t.Errorf("expiry = %v, want %v", loaded.Expiry, expiry)
			}
		})
	}
}

func TestLoadFailuresKeepServiceContext(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string, string)
		want    string
	}{
		{
			name: "missing client file",
			prepare: func(t *testing.T, _, tokenPath string) {
				writeJSON(t, tokenPath, Token{RefreshToken: "refresh-token"})
			},
			want: "Example client credentials를 읽을 수 없습니다",
		},
		{
			name: "malformed client file",
			prepare: func(t *testing.T, clientPath, tokenPath string) {
				if err := os.WriteFile(clientPath, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				writeJSON(t, tokenPath, Token{RefreshToken: "refresh-token"})
			},
			want: "Example client credentials 파싱 실패",
		},
		{
			name: "incomplete client credentials",
			prepare: func(t *testing.T, clientPath, tokenPath string) {
				writeJSON(t, clientPath, map[string]any{
					"installed": map[string]string{"client_id": "client-id"},
				})
				writeJSON(t, tokenPath, Token{RefreshToken: "refresh-token"})
			},
			want: "Example client credentials에 client_id/client_secret이 없습니다",
		},
		{
			name: "missing token file",
			prepare: func(t *testing.T, clientPath, _ string) {
				writeJSON(t, clientPath, map[string]any{
					"installed": map[string]string{"client_id": "client-id", "client_secret": "secret"},
				})
			},
			want: "Example 토큰을 읽을 수 없습니다",
		},
		{
			name: "malformed token file",
			prepare: func(t *testing.T, clientPath, tokenPath string) {
				writeJSON(t, clientPath, map[string]any{
					"installed": map[string]string{"client_id": "client-id", "client_secret": "secret"},
				})
				if err := os.WriteFile(tokenPath, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "Example 토큰 파싱 실패",
		},
		{
			name: "missing refresh token",
			prepare: func(t *testing.T, clientPath, tokenPath string) {
				writeJSON(t, clientPath, map[string]any{
					"installed": map[string]string{"client_id": "client-id", "client_secret": "secret"},
				})
				writeJSON(t, tokenPath, Token{AccessToken: "access-token"})
			},
			want: "Example 토큰에 refresh_token이 없습니다",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			clientPath := filepath.Join(dir, "client.json")
			tokenPath := filepath.Join(dir, "token.json")
			test.prepare(t, clientPath, tokenPath)

			_, err := Load("Example", clientPath, tokenPath)
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestRefreshReportsAndRetainsRotatedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.FormValue("client_id"); got != "client-id" {
			t.Errorf("client_id = %q", got)
		}
		if got := r.FormValue("refresh_token"); got != "old-refresh" {
			t.Errorf("refresh_token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "new-access",
			"refresh_token": "rotated-refresh",
			"expires_in": 3600
		}`))
	}))
	defer server.Close()

	before := time.Now()
	result, err := Refresh(context.Background(), server.Client(), RefreshRequest{
		TokenURL:     server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "old-refresh",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.AccessToken != "new-access" {
		t.Errorf("access token = %q", result.AccessToken)
	}
	if result.RefreshToken != "rotated-refresh" || !result.Rotated {
		t.Errorf("refresh token = %q, rotated = %v", result.RefreshToken, result.Rotated)
	}
	if result.Expiry.Before(before.Add(3599*time.Second)) || result.Expiry.After(time.Now().Add(3601*time.Second)) {
		t.Errorf("expiry = %v, want about one hour from now", result.Expiry)
	}
}

func TestRefreshKeepsCurrentTokenWhenResponseOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
	}))
	defer server.Close()

	result, err := Refresh(context.Background(), server.Client(), RefreshRequest{
		TokenURL:     server.URL,
		RefreshToken: "current-refresh",
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.RefreshToken != "current-refresh" || result.Rotated {
		t.Errorf("refresh token = %q, rotated = %v", result.RefreshToken, result.Rotated)
	}
}

func TestPersistAtomicallyReplacesToken(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(tokenPath, []byte("old-token"), 0o644); err != nil {
		t.Fatal(err)
	}

	want := StoredToken("new-access", "new-refresh", time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC))
	if err := Persist("Example", tokenPath, want); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	var got Token
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if got != want {
		t.Errorf("token = %+v, want %+v", got, want)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	if _, err := os.Stat(tokenPath + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary token remains: %v", err)
	}
}

func TestPersistRenameFailurePreservesPreviousToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")
	oldData := []byte(`{"refresh_token":"old-refresh"}`)
	if err := os.WriteFile(tokenPath, oldData, 0o600); err != nil {
		t.Fatal(err)
	}

	renameErr := errors.New("injected rename failure")
	err := persist(tokenPath, StoredToken("new-access", "new-refresh", time.Now()), fileOps{
		writeFile: os.WriteFile,
		rename: func(_, _ string) error {
			return renameErr
		},
		remove: os.Remove,
	})
	if err == nil {
		t.Fatal("persist succeeded, want error")
	}
	var persistErr *PersistError
	if !errors.As(err, &persistErr) || persistErr.Stage != PersistRename || !errors.Is(err, renameErr) {
		t.Fatalf("error = %#v, want rename PersistError", err)
	}

	got, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(oldData) {
		t.Errorf("previous token changed after failed rename: %s", got)
	}
	if _, err := os.Stat(tokenPath + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary token remains after failed rename: %v", err)
	}
}

func TestPersistReportsWriteFailure(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "missing", "token.json")
	err := Persist("Example", tokenPath, StoredToken("access", "refresh", time.Now()))
	if err == nil {
		t.Fatal("Persist succeeded, want error")
	}
	var persistErr *PersistError
	if !errors.As(err, &persistErr) || persistErr.Stage != PersistWrite {
		t.Fatalf("error = %#v, want write PersistError", err)
	}
}
