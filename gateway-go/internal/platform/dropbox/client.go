// Package dropbox is a minimal Dropbox v2 API client with auto-refreshing
// OAuth2 tokens, mirroring the gmail/calendar platform clients. It uses the
// PKCE authorization-code flow so the gateway never needs to store an app
// secret: the long-lived refresh token (issued once via cmd/deneb-dropbox-auth)
// is exchanged for short-lived access tokens on demand.
//
// Two hosts are involved, per Dropbox's API split:
//   - api.dropboxapi.com     — JSON RPC endpoints (list_folder, search, sharing)
//   - content.dropboxapi.com — file content up/download (Dropbox-API-Arg header)
package dropbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"unicode/utf16"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	defaultAuthorizeURL = "https://www.dropbox.com/oauth2/authorize"
	defaultTokenURL     = "https://api.dropboxapi.com/oauth2/token" //nolint:gosec // G101 false positive — public OAuth endpoint, not a credential
	defaultAPIHost      = "https://api.dropboxapi.com"
	defaultContentHost  = "https://content.dropboxapi.com"
)

// DefaultScopes are the OAuth scopes Deneb requests: read/write file content,
// read metadata (list/search), and create shared links.
var DefaultScopes = []string{
	"files.metadata.read",
	"files.content.read",
	"files.content.write",
	"sharing.write",
}

// appCredentials matches dropbox_app.json.
type appCredentials struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret,omitempty"`
}

// tokenJSON matches dropbox_token.json (and the OAuth token response).
type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

// Client is a Dropbox API client with auto-refreshing OAuth2 tokens.
//
// The host fields default to the public Dropbox endpoints; tests override them
// to point at an httptest server (kept on the struct rather than package vars so
// the race detector stays quiet under -race).
type Client struct {
	mu           sync.Mutex
	appKey       string
	appSecret    string // optional; empty for pure PKCE public apps
	accessToken  string
	refreshToken string
	expiry       time.Time
	tokenPath    string
	httpClient   *http.Client

	tokenURL    string
	apiHost     string
	contentHost string
}

var (
	globalMu     sync.Mutex
	globalClient *Client
)

// DefaultClient returns the singleton Dropbox client, initializing on first
// call. Like the gmail client, a failed init can be retried on the next call.
func DefaultClient() (*Client, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalClient != nil {
		return globalClient, nil
	}

	c, err := newClientFromDir(CredentialsDir())
	if err != nil {
		return nil, err
	}
	globalClient = c
	return globalClient, nil
}

// CredentialsDir returns the shared Deneb credentials directory
// (~/.deneb/credentials), the same location gmail/calendar use.
func CredentialsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deneb", "credentials")
}

const (
	appFileName   = "dropbox_app.json"
	tokenFileName = "dropbox_token.json" //nolint:gosec // G101 false positive — filename, not a credential
)

// newClientFromDir loads app credentials and token from the given directory.
func newClientFromDir(dir string) (*Client, error) {
	appPath := filepath.Join(dir, appFileName)
	tokenPath := filepath.Join(dir, tokenFileName)

	appData, err := os.ReadFile(appPath)
	if err != nil {
		return nil, fmt.Errorf("Dropbox 앱 정보를 읽을 수 없습니다 (%s): %w", appPath, err) //nolint:staticcheck // ST1005 — Korean error message
	}
	var app appCredentials
	if err := json.Unmarshal(appData, &app); err != nil {
		return nil, fmt.Errorf("Dropbox 앱 정보 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if app.AppKey == "" {
		return nil, fmt.Errorf("Dropbox 앱 정보에 app_key가 없습니다") //nolint:staticcheck // ST1005 — Korean error message
	}

	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("Dropbox 토큰을 읽을 수 없습니다 (%s): %w", tokenPath, err) //nolint:staticcheck // ST1005 — Korean error message
	}
	var tok tokenJSON
	if err := json.Unmarshal(tokenData, &tok); err != nil {
		return nil, fmt.Errorf("Dropbox 토큰 파싱 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("Dropbox 토큰에 refresh_token이 없습니다 (cmd/deneb-dropbox-auth로 재발급하세요)") //nolint:staticcheck // ST1005 — Korean error message
	}

	var expiry time.Time
	if tok.Expiry != "" {
		expiry, _ = time.Parse(time.RFC3339, tok.Expiry)
	}

	return &Client{
		appKey:       app.AppKey,
		appSecret:    app.AppSecret,
		accessToken:  tok.AccessToken,
		refreshToken: tok.RefreshToken,
		expiry:       expiry,
		tokenPath:    tokenPath,
		httpClient:   httputil.NewClient(60 * time.Second),
		tokenURL:     defaultTokenURL,
		apiHost:      defaultAPIHost,
		contentHost:  defaultContentHost,
	}, nil
}

// validToken returns the current access token, refreshing if expired or within
// 60s of expiry. The caller's context bounds the refresh latency.
func (c *Client) validToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Until(c.expiry) > 60*time.Second {
		return c.accessToken, nil
	}
	return c.refresh(ctx)
}

// refresh exchanges the refresh token for a new access token. Dropbox refresh
// tokens are long-lived and not rotated, so only the access token and expiry
// change. Must be called with c.mu held.
func (c *Client) refresh(ctx context.Context) (string, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
		"client_id":     {c.appKey},
	}
	// Confidential apps authenticate the refresh with the secret; PKCE public
	// apps send only client_id.
	if c.appSecret != "" {
		data.Set("client_secret", c.appSecret)
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, c.tokenURL, strings.NewReader(data.Encode()))
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
		return "", fmt.Errorf("토큰 갱신 실패 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var tok tokenJSON
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("토큰 응답 파싱 실패: %w", err)
	}

	c.accessToken = tok.AccessToken
	c.expiry = time.Now().Add(tokenTTL(tok.ExpiresIn))
	// Dropbox normally does not rotate refresh tokens, but capture a rotated one
	// defensively so a policy change can't permanently lock us out (gmail pattern).
	if tok.RefreshToken != "" && tok.RefreshToken != c.refreshToken {
		slog.Info("Dropbox refresh token rotated", "tokenPath", c.tokenPath)
		c.refreshToken = tok.RefreshToken
	}
	c.persistToken()

	return c.accessToken, nil
}

// persistToken writes the current token state to disk atomically (temp+rename,
// 0600). Failures are user-observable on the next restart, so log at Error.
func (c *Client) persistToken() {
	tok := tokenJSON{
		AccessToken:  c.accessToken,
		RefreshToken: c.refreshToken,
		Expiry:       c.expiry.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // G117 false positive — not a secret
	if err != nil {
		slog.Error("Dropbox token marshal failed — token may be stale on restart",
			"tokenPath", c.tokenPath, "error", err)
		return
	}
	// atomicfile does flock + temp + rename; the flock serializes writes across
	// the worktrees that share ~/.deneb/credentials.
	if err := atomicfile.WriteFile(c.tokenPath, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		slog.Error("Dropbox token write failed — token may be stale on restart",
			"tokenPath", c.tokenPath, "error", err)
	}
}

// doRPC performs an authenticated JSON RPC call to api.dropboxapi.com and
// returns the raw response body. Non-2xx responses are returned as errors with
// the Dropbox error_summary truncated.
func (c *Client) doRPC(ctx context.Context, path string, payload any) ([]byte, error) {
	token, err := c.validToken(ctx)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(raw))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiHost+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Dropbox API 응답 읽기 실패: %w", err) //nolint:staticcheck // ST1005 — Korean error message
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

// APIError carries a non-2xx Dropbox response so callers can branch on specific
// conditions (e.g. 409 shared_link_already_exists).
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Dropbox API 오류 (HTTP %d): %s", e.StatusCode, truncate(e.Body, 500))
}

// doContent performs an authenticated content-endpoint call to
// content.dropboxapi.com. apiArg is JSON-encoded into the Dropbox-API-Arg
// header (ASCII-escaped). For uploads, body holds the file bytes; for
// downloads, body is nil and the response body carries the file content.
func (c *Client) doContent(ctx context.Context, path string, apiArg any, body []byte) (*http.Response, error) {
	token, err := c.validToken(ctx)
	if err != nil {
		return nil, err
	}

	argJSON, err := json.Marshal(apiArg)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.contentHost+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Dropbox-API-Arg must be ASCII-only; Korean paths/filenames need \uXXXX.
	req.Header.Set("Dropbox-API-Arg", asciiEscapeJSON(argJSON))
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	return c.httpClient.Do(req)
}

// --- PKCE helpers (shared with cmd/deneb-dropbox-auth) ---

// GeneratePKCE returns a fresh (code_verifier, code_challenge) pair for the
// PKCE S256 flow. The verifier is a 64-byte URL-safe random string; the
// challenge is base64url(SHA256(verifier)).
func GeneratePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// AuthorizeURL builds the consent URL for the PKCE flow. With no redirect_uri,
// Dropbox shows the authorization code on screen for the user to copy.
func AuthorizeURL(appKey, challenge string, scopes []string) string {
	q := url.Values{
		"client_id":             {appKey},
		"response_type":         {"code"},
		"token_access_type":     {"offline"}, // required to receive a refresh_token
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {strings.Join(scopes, " ")},
	}
	return defaultAuthorizeURL + "?" + q.Encode()
}

// TokenResult is the outcome of an authorization-code exchange.
type TokenResult struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// ExchangeCode trades an authorization code (+ PKCE verifier) for tokens. For a
// confidential app, pass appSecret; for a pure PKCE public app, leave it empty.
func ExchangeCode(ctx context.Context, appKey, appSecret, code, verifier string) (*TokenResult, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {strings.TrimSpace(code)},
		"client_id":     {appKey},
		"code_verifier": {verifier},
	}
	if appSecret != "" {
		data.Set("client_secret", appSecret)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, defaultTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httputil.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("토큰 교환 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("토큰 교환 실패 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	var tok tokenJSON
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("토큰 응답 파싱 실패: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("응답에 refresh_token이 없습니다 (token_access_type=offline 확인)") //nolint:staticcheck // ST1005 — Korean error message
	}
	return &TokenResult{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       time.Now().Add(tokenTTL(tok.ExpiresIn)),
	}, nil
}

// LoadApp reads app_key/app_secret from dropbox_app.json in dir, if present.
func LoadApp(dir string) (appKey, appSecret string, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, appFileName))
	if err != nil {
		return "", "", false
	}
	var app appCredentials
	if json.Unmarshal(data, &app) != nil || app.AppKey == "" {
		return "", "", false
	}
	return app.AppKey, app.AppSecret, true
}

// SaveApp writes dropbox_app.json (0600) into dir.
func SaveApp(dir, appKey, appSecret string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(appCredentials{AppKey: appKey, AppSecret: appSecret}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, appFileName), data, 0o600)
}

// SaveToken writes dropbox_token.json (0600) into dir.
func SaveToken(dir string, tr *TokenResult) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tok := tokenJSON{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       tr.Expiry.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(tok, "", "  ") //nolint:gosec // G117 false positive — not a secret
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, tokenFileName), data, 0o600)
}

// HasToken reports whether usable Dropbox credentials (app key + refresh token)
// exist on disk. Used to decide whether to seed the backup cron enabled.
func HasToken() bool {
	dir := CredentialsDir()
	if _, _, ok := LoadApp(dir); !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		return false
	}
	var tok tokenJSON
	return json.Unmarshal(data, &tok) == nil && tok.RefreshToken != ""
}

// --- small utilities ---

// asciiEscapeJSON rewrites any non-ASCII rune in a JSON byte slice as \uXXXX so
// the result is safe for the Dropbox-API-Arg HTTP header (HTTP headers are
// latin-1; Dropbox rejects raw UTF-8). Surrogate pairs handle astral chars.
func asciiEscapeJSON(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, r := range string(b) {
		// Pass printable ASCII through; escape 0x7F (DEL) and all non-ASCII per
		// Dropbox's Dropbox-API-Arg encoding spec. Control chars <0x20 never
		// reach here — json.Marshal already escaped them.
		if r < 0x7F {
			sb.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&sb, "\\u%04x\\u%04x", r1, r2)
			continue
		}
		fmt.Fprintf(&sb, "\\u%04x", r)
	}
	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Back up to a UTF-8 rune boundary so Korean error text isn't split
	// mid-codepoint (byte-vs-rune truncation class).
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen] + "..."
}

// tokenTTL converts an OAuth expires_in (seconds) to a Duration, defaulting to
// ~4h when the server omits or zeroes it — guarding against an expiry==now
// refresh loop where every API call re-hits the token endpoint.
func tokenTTL(expiresIn int64) time.Duration {
	if expiresIn <= 0 {
		expiresIn = 14400
	}
	return time.Duration(expiresIn) * time.Second
}
