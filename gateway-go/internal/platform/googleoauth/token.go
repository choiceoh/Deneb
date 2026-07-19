// Package googleoauth owns the credential-file and token lifecycle shared by
// Google API clients. Service clients remain responsible for their API calls
// and for serializing refreshes with their own mutex.
package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxTokenResponseBytes = 1 << 20
	refreshTimeout        = 10 * time.Second
)

// Token matches the Google OAuth2 token JSON format used on disk and by the
// token endpoint. Keep these field names stable: existing Calendar and Gmail
// credential files use this exact shape.
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
}

// Loaded contains the credentials and token state read from a Google service's
// two credential files.
type Loaded struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Source owns one Google service's in-memory token state. It serializes refresh
// and persistence so concurrent API calls never race a token rotation.
type Source struct {
	mu           sync.Mutex
	service      string
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
	expiry       time.Time
	tokenPath    string
	httpClient   *http.Client
}

// NewSource initializes a refreshable token source from loaded credentials.
func NewSource(service string, loaded Loaded, tokenPath string, httpClient *http.Client) *Source {
	return &Source{
		service:      service,
		clientID:     loaded.ClientID,
		clientSecret: loaded.ClientSecret,
		accessToken:  loaded.AccessToken,
		refreshToken: loaded.RefreshToken,
		expiry:       loaded.Expiry,
		tokenPath:    tokenPath,
		httpClient:   httpClient,
	}
}

// ValidToken returns a token with at least one minute of validity, refreshing
// and persisting it when necessary.
func (s *Source) ValidToken(ctx context.Context, tokenURL string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && time.Until(s.expiry) > time.Minute {
		return s.accessToken, nil
	}
	refreshed, err := Refresh(ctx, s.httpClient, RefreshRequest{
		TokenURL:     tokenURL,
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		RefreshToken: s.refreshToken,
	})
	if err != nil {
		return "", err
	}

	s.accessToken = refreshed.AccessToken
	s.expiry = refreshed.Expiry
	if refreshed.Rotated {
		slog.Info(s.service+" refresh token rotated by Google", "tokenPath", s.tokenPath)
	}
	s.refreshToken = refreshed.RefreshToken
	s.persistLocked()
	return s.accessToken, nil
}

// State returns a consistent snapshot for diagnostics and service-level tests.
func (s *Source) State() (accessToken, refreshToken string, expiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accessToken, s.refreshToken, s.expiry
}

// Credentials returns the immutable OAuth client identifier and secret.
func (s *Source) Credentials() (clientID, clientSecret string) {
	return s.clientID, s.clientSecret
}

// TokenPath returns the file used for persistence.
func (s *Source) TokenPath() string { return s.tokenPath }

// Persist writes the current token state. Errors are logged by Persist and do
// not invalidate the in-memory token, matching refresh-time behavior.
func (s *Source) Persist() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistLocked()
}

func (s *Source) persistLocked() {
	_ = Persist(s.service, s.tokenPath, StoredToken(s.accessToken, s.refreshToken, s.expiry))
}

type clientCredentials struct {
	Installed *clientConfig `json:"installed"`
	Web       *clientConfig `json:"web"`
}

type clientConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Load reads a Google OAuth client file and token file. service is used only
// in operator-facing errors so callers retain their existing Calendar/Gmail
// wording.
func Load(service, clientPath, tokenPath string) (Loaded, error) {
	clientData, err := os.ReadFile(clientPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("%s client credentials를 읽을 수 없습니다 (%s): %w", service, clientPath, err)
	}

	var credentials clientCredentials
	if err := json.Unmarshal(clientData, &credentials); err != nil {
		return Loaded{}, fmt.Errorf("%s client credentials 파싱 실패: %w", service, err)
	}
	config := credentials.Installed
	if config == nil {
		config = credentials.Web
	}
	if config == nil || config.ClientID == "" || config.ClientSecret == "" {
		return Loaded{}, fmt.Errorf("%s client credentials에 client_id/client_secret이 없습니다", service)
	}

	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		return Loaded{}, fmt.Errorf("%s 토큰을 읽을 수 없습니다 (%s): %w", service, tokenPath, err)
	}
	var token Token
	if err := json.Unmarshal(tokenData, &token); err != nil {
		return Loaded{}, fmt.Errorf("%s 토큰 파싱 실패: %w", service, err)
	}
	if token.RefreshToken == "" {
		return Loaded{}, fmt.Errorf("%s 토큰에 refresh_token이 없습니다", service)
	}

	var expiry time.Time
	if token.Expiry != "" {
		expiry, _ = time.Parse(time.RFC3339, token.Expiry)
	}

	return Loaded{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       expiry,
	}, nil
}

// RefreshRequest is the minimum state needed to exchange a refresh token.
type RefreshRequest struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	RefreshToken string
}

// RefreshResult contains the new access-token state. RefreshToken is always
// populated: when Google omits it, the current token is retained. Rotated is
// true only when Google supplied a different refresh token.
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Rotated      bool
}

// Refresh exchanges a refresh token for a new access token. The caller owns
// synchronization and persistence so the in-memory update and disk write can
// remain one serialized operation.
func Refresh(ctx context.Context, client *http.Client, input RefreshRequest) (RefreshResult, error) {
	data := url.Values{
		"client_id":     {input.ClientID},
		"client_secret": {input.ClientSecret},
		"refresh_token": {input.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	refreshCtx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, input.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return RefreshResult{}, fmt.Errorf("토큰 갱신 요청 생성 실패: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("토큰 갱신 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return RefreshResult{}, fmt.Errorf("토큰 응답 읽기 실패: %w", err)
	}
	if len(body) > maxTokenResponseBytes {
		return RefreshResult{}, fmt.Errorf("토큰 응답이 비정상적으로 큼 (>%dB)", maxTokenResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return RefreshResult{}, fmt.Errorf("토큰 갱신 실패 (HTTP %d): %s", resp.StatusCode, truncate(string(body), 500))
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return RefreshResult{}, fmt.Errorf("토큰 응답 파싱 실패: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return RefreshResult{}, fmt.Errorf("토큰 응답에 access_token이 없습니다")
	}

	rotated := token.RefreshToken != "" && token.RefreshToken != input.RefreshToken
	if token.RefreshToken == "" {
		token.RefreshToken = input.RefreshToken
	}
	return RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
		Rotated:      rotated,
	}, nil
}

// StoredToken builds the stable on-disk token shape used by both services.
func StoredToken(accessToken, refreshToken string, expiry time.Time) Token {
	return Token{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		RefreshToken: refreshToken,
		Expiry:       expiry.Format(time.RFC3339),
	}
}

// PersistStage identifies which part of the atomic token write failed.
type PersistStage string

const (
	PersistMarshal PersistStage = "marshal"
	PersistWrite   PersistStage = "write"
	PersistRename  PersistStage = "rename"
)

// PersistError retains the paths needed for the existing operator logs.
type PersistError struct {
	Stage     PersistStage
	TokenPath string
	TempPath  string
	Err       error
}

// Error returns the human-readable error message.
func (e *PersistError) Error() string {
	return fmt.Sprintf("persist OAuth token (%s): %v", e.Stage, e.Err)
}

// Unwrap returns the persistence error's underlying cause.
func (e *PersistError) Unwrap() error { return e.Err }

type fileOps struct {
	writeFile func(string, []byte, os.FileMode) error
	rename    func(string, string) error
	remove    func(string) error
}

// Persist atomically stores a token with mode 0600. It logs failures using the
// service-specific wording callers historically exposed and also returns the
// error for direct health checks and tests.
func Persist(service, tokenPath string, token Token) error {
	err := persist(tokenPath, token, fileOps{
		writeFile: os.WriteFile,
		rename:    os.Rename,
		remove:    os.Remove,
	})
	if err == nil {
		return nil
	}

	var persistErr *PersistError
	if !errors.As(err, &persistErr) {
		slog.Error(service+" token persist failed — refresh token may be stale on restart",
			"tokenPath", tokenPath, "error", err)
		return err
	}

	switch persistErr.Stage {
	case PersistMarshal:
		slog.Error(service+" token marshal failed — refresh token may be stale on restart",
			"tokenPath", persistErr.TokenPath, "error", persistErr.Err)
	case PersistWrite:
		slog.Error(service+" token write failed — refresh token may be stale on restart",
			"tmp", persistErr.TempPath, "tokenPath", persistErr.TokenPath, "error", persistErr.Err)
	case PersistRename:
		slog.Error(service+" token rename failed — refresh token may be stale on restart",
			"tmp", persistErr.TempPath, "tokenPath", persistErr.TokenPath, "error", persistErr.Err)
	}
	return err
}

func persist(tokenPath string, token Token, files fileOps) error {
	data, err := json.MarshalIndent(token, "", "  ") //nolint:gosec // G117 false positive — not a secret
	if err != nil {
		return &PersistError{Stage: PersistMarshal, TokenPath: tokenPath, Err: err}
	}

	tempPath := tokenPath + ".tmp"
	if err := files.writeFile(tempPath, data, 0o600); err != nil {
		return &PersistError{Stage: PersistWrite, TokenPath: tokenPath, TempPath: tempPath, Err: err}
	}
	if err := files.rename(tempPath, tokenPath); err != nil {
		_ = files.remove(tempPath)
		return &PersistError{Stage: PersistRename, TokenPath: tokenPath, TempPath: tempPath, Err: err}
	}
	return nil
}

func truncate(value string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	runes := []rune(value)
	if len(runes) <= maxLen {
		return value
	}
	return string(runes[:maxLen]) + "..."
}
