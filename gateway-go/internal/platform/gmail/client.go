package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

var tokenURL = "https://oauth2.googleapis.com/token"

const apiBase = "https://gmail.googleapis.com/gmail/v1/users/me"

// setTokenURL overrides the token endpoint URL (for testing).
func setTokenURL(u string) { tokenURL = u }

// clientCredentials matches the Google OAuth2 client_secret JSON format.
type clientCredentials struct {
	Installed *oauthClientConfig `json:"installed"`
	Web       *oauthClientConfig `json:"web"`
}

type oauthClientConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// tokenJSON matches the standard Google OAuth2 token JSON format.
type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

// Client is a Gmail API client with auto-refreshing OAuth2 tokens.
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

// DefaultClient returns the singleton Gmail client, initializing on first call.
// Unlike sync.Once, a failed initialization can be retried on the next call.
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
	dir := credentialsDir()
	return newClientFromDir(dir)
}

// newClientFromDir loads credentials and token from the given directory.
// Extracted for testability.
func newClientFromDir(dir string) (*Client, error) {
	clientPath := filepath.Join(dir, "gmail_client.json")
	tokenPath := filepath.Join(dir, "gmail_token.json")

	// Load client credentials.
	clientData, err := os.ReadFile(clientPath)
	if err != nil {
		return nil, fmt.Errorf("Gmail client credentials를 읽을 수 없습니다 (%s): %w", clientPath, err)
	}
	var creds clientCredentials
	if err := json.Unmarshal(clientData, &creds); err != nil {
		return nil, fmt.Errorf("Gmail client credentials 파싱 실패: %w", err)
	}
	cfg := creds.Installed
	if cfg == nil {
		cfg = creds.Web
	}
	if cfg == nil || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("Gmail client credentials에 client_id/client_secret이 없습니다")
	}

	// Load token.
	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("Gmail 토큰을 읽을 수 없습니다 (%s): %w", tokenPath, err)
	}
	var tok tokenJSON
	if err := json.Unmarshal(tokenData, &tok); err != nil {
		return nil, fmt.Errorf("Gmail 토큰 파싱 실패: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("Gmail 토큰에 refresh_token이 없습니다")
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
func (c *Client) validToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Refresh if expired or within 60s of expiry.
	if c.accessToken != "" && time.Until(c.expiry) > 60*time.Second {
		return c.accessToken, nil
	}
	return c.refresh()
}

// refresh exchanges the refresh token for a new access token.
func (c *Client) refresh() (string, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := c.httpClient.PostForm(tokenURL, data)
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
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}

	// Persist updated token to disk.
	c.persistToken()

	return c.accessToken, nil
}

// persistToken writes the current token state to disk atomically.
func (c *Client) persistToken() {
	tok := tokenJSON{
		AccessToken:  c.accessToken,
		TokenType:    "Bearer",
		RefreshToken: c.refreshToken,
		Expiry:       c.expiry.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return
	}
	// Atomic write via temp file + rename to prevent corruption on crash.
	tmp := c.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, c.tokenPath) // best-effort: token persist failure is non-critical
}

// doAPI performs an authenticated HTTP request to the Gmail API.
func (c *Client) doAPI(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.validToken()
	if err != nil {
		return nil, err
	}

	reqURL := apiBase + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// readJSON performs a GET request and decodes the JSON response into dest.
func (c *Client) readJSON(ctx context.Context, path string, dest any) error {
	resp, err := c.doAPI(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Gmail API 응답 읽기 실패: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Gmail API 오류 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	return json.Unmarshal(body, dest)
}

// postJSON performs a POST request with a JSON body and decodes the response.
func (c *Client) postJSON(ctx context.Context, path string, payload any, dest any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doAPI(ctx, "POST", path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Gmail API 응답 읽기 실패: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Gmail API 오류 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	if dest != nil {
		return json.Unmarshal(body, dest)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
