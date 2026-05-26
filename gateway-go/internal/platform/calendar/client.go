package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// tokenURL is the Google OAuth2 endpoint; overridable in tests.
var tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential

// apiBase is the Calendar v3 root for the authenticated user's primary calendar.
const apiBase = "https://www.googleapis.com/calendar/v3"

// setTokenURL overrides the token endpoint URL (for testing).
func setTokenURL(u string) { tokenURL = u }

// clientCredentials matches Google's OAuth2 client_secret JSON file.
type clientCredentials struct {
	Installed *oauthClientConfig `json:"installed"`
	Web       *oauthClientConfig `json:"web"`
}

type oauthClientConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// tokenJSON matches Google's OAuth2 token storage shape.
type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

// Client is a Calendar API client with auto-refreshing OAuth2 tokens.
// The credential/token file pair lives separately from Gmail's to keep
// scopes orthogonal — same Google Cloud project, different OAuth flow.
type Client struct {
	mu           sync.Mutex
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
	expiry       time.Time
	tokenPath    string
	httpClient   *http.Client
}

var (
	globalMu     sync.Mutex
	globalClient *Client
)

// DefaultClient returns the singleton Calendar client, initializing on
// first call. Mirrors gmail.DefaultClient: failed init can be retried.
func DefaultClient() (*Client, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalClient != nil {
		return globalClient, nil
	}

	c, err := newClient()
	if err != nil {
		return nil, err
	}
	globalClient = c
	return globalClient, nil
}

func credentialsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deneb", "credentials")
}

func newClient() (*Client, error) {
	return newClientFromDir(credentialsDir())
}

// newClientFromDir loads credentials + token from the given directory.
// Extracted for tests; the live caller uses ~/.deneb/credentials.
func newClientFromDir(dir string) (*Client, error) {
	clientPath := filepath.Join(dir, "calendar_client.json")
	tokenPath := filepath.Join(dir, "calendar_token.json")

	clientData, err := os.ReadFile(clientPath)
	if err != nil {
		return nil, fmt.Errorf("Calendar client credentials를 읽을 수 없습니다 (%s): %w", clientPath, err) //nolint:staticcheck // ST1005 — Korean error message
	}
	var creds clientCredentials
	if err := json.Unmarshal(clientData, &creds); err != nil {
		return nil, fmt.Errorf("Calendar client credentials 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	cfg := creds.Installed
	if cfg == nil {
		cfg = creds.Web
	}
	if cfg == nil || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("Calendar client credentials에 client_id/client_secret이 없습니다") //nolint:staticcheck // ST1005 — Korean error message
	}

	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("Calendar 토큰을 읽을 수 없습니다 (%s): %w", tokenPath, err) //nolint:staticcheck // ST1005 — Korean error message
	}
	var tok tokenJSON
	if err := json.Unmarshal(tokenData, &tok); err != nil {
		return nil, fmt.Errorf("Calendar 토큰 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("Calendar 토큰에 refresh_token이 없습니다") //nolint:staticcheck // ST1005 — Korean error message
	}

	var expiry time.Time
	if tok.Expiry != "" {
		expiry, _ = time.Parse(time.RFC3339, tok.Expiry)
	}

	return &Client{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		accessToken:  tok.AccessToken,
		refreshToken: tok.RefreshToken,
		expiry:       expiry,
		tokenPath:    tokenPath,
		httpClient:   httputil.NewClient(60 * time.Second),
	}, nil
}

// validToken returns the current access token, refreshing if needed.
func (c *Client) validToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Until(c.expiry) > 60*time.Second {
		return c.accessToken, nil
	}
	return c.refresh(ctx)
}

func (c *Client) refresh(ctx context.Context) (string, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("토큰 갱신 요청 생성 실패: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("토큰 갱신 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("토큰 응답 읽기 실패: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("토큰 갱신 실패 (HTTP %d): %s", resp.StatusCode, body)
	}

	var tok tokenJSON
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("토큰 응답 파싱 실패: %w", err)
	}

	c.accessToken = tok.AccessToken
	c.expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if tok.RefreshToken != "" && tok.RefreshToken != c.refreshToken {
		// Same rotation rule as Gmail: persist immediately or the next
		// restart will load a revoked refresh token.
		slog.Info("Calendar refresh token rotated by Google", "tokenPath", c.tokenPath)
		c.refreshToken = tok.RefreshToken
	}

	c.persistToken()
	return c.accessToken, nil
}

func (c *Client) persistToken() {
	tok := tokenJSON{
		AccessToken:  c.accessToken,
		TokenType:    "Bearer",
		RefreshToken: c.refreshToken,
		Expiry:       c.expiry.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // G117 false positive — not a secret
	if err != nil {
		slog.Error("Calendar token marshal failed — refresh token may be stale on restart",
			"tokenPath", c.tokenPath, "error", err)
		return
	}
	tmp := c.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Error("Calendar token write failed — refresh token may be stale on restart",
			"tmp", tmp, "tokenPath", c.tokenPath, "error", err)
		return
	}
	if err := os.Rename(tmp, c.tokenPath); err != nil {
		slog.Error("Calendar token rename failed — refresh token may be stale on restart",
			"tmp", tmp, "tokenPath", c.tokenPath, "error", err)
		_ = os.Remove(tmp)
	}
}

func (c *Client) doAPI(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.validToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) readJSON(ctx context.Context, path string, dest any) error {
	resp, err := c.doAPI(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Calendar API 응답 읽기 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Calendar API 오류 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500)) //nolint:staticcheck // ST1005 — Korean error message
	}
	return json.Unmarshal(body, dest)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
