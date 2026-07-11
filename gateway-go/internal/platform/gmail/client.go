package gmail

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/googleoauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

var tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential

const apiBase = "https://gmail.googleapis.com/gmail/v1/users/me"

// Response-size bounds: every external HTTP body read is capped so a runaway or
// malformed upstream (misbehaving proxy, endless stream) cannot balloon the
// always-on gateway's memory. Generous by design — Workspace Enterprise Plus
// accepts messages up to 70 MB, and a single attachment near that limit is
// ~94 MiB once base64-wrapped in the API JSON (GetAttachment rides readJSON),
// so the API cap must never clip a legitimate payload. Readers fetch limit+1
// and fail with an explicit over-limit error instead of a confusing JSON one.
const maxAPIResponseBytes = 128 << 20 // Gmail API reads (base64-wrapped attachments)

// setTokenURL overrides the token endpoint URL (for testing).
func setTokenURL(u string) { tokenURL = u }

// tokenJSON matches the standard Google OAuth2 token JSON format.
type tokenJSON = googleoauth.Token

// Client is a Gmail API client with auto-refreshing OAuth2 tokens.
type Client struct {
	mu           sync.Mutex
	tokens       *googleoauth.Source
	httpClient   *http.Client
	accountEmail string // cached authenticated address (users/me/profile)
}

// AccountEmail returns the authenticated account's own address (the tracked
// mailbox), fetched once from users/me/profile and cached. Lets callers send to
// "self" without knowing the literal address. The cache is read/written under
// mu, but the network read runs WITHOUT mu held (doAPI→validToken takes mu, and
// sync.Mutex is not reentrant).
func (c *Client) AccountEmail(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.accountEmail
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	var profile struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := c.readJSON(ctx, "/profile", &profile); err != nil {
		return "", err
	}
	if profile.EmailAddress == "" {
		return "", fmt.Errorf("Gmail 프로필에 이메일 주소가 없습니다") //nolint:staticcheck // ST1005 — Korean error message
	}

	c.mu.Lock()
	c.accountEmail = profile.EmailAddress
	c.mu.Unlock()
	return profile.EmailAddress, nil
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

	loaded, err := googleoauth.Load("Gmail", clientPath, tokenPath)
	if err != nil {
		return nil, err
	}

	httpClient := httputil.NewClient(60 * time.Second)
	return &Client{
		tokens:     googleoauth.NewSource("Gmail", loaded, tokenPath, httpClient),
		httpClient: httpClient,
	}, nil
}

// validToken returns the current access token, refreshing if needed.
// Uses the caller's context so a slow OAuth endpoint cannot outlive the
// parent request deadline. A 10s floor is applied on top of the caller's
// deadline to bound refresh latency.
func (c *Client) validToken(ctx context.Context) (string, error) {
	return c.tokens.ValidToken(ctx, tokenURL)
}

// persistToken writes the current token state to disk atomically.
//
// Failures here are user-observable on the next gateway restart: if Google
// has rotated the refresh token but the new one never reaches disk, the old
// (revoked) token will be reloaded and every Gmail call will return
// "unauthorized" until the user re-runs the OAuth flow. Surface every failure
// at Error level so the operator can react before tokens drift.
func (c *Client) persistToken() {
	c.tokens.Persist()
}

// doAPI performs an authenticated HTTP request to the Gmail API.
func (c *Client) doAPI(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return googleoauth.DoBearer(ctx, c.httpClient, c.validToken, apiBase, method, path, body)
}

// readJSON performs a GET request and decodes the JSON response into dest.
func (c *Client) readJSON(ctx context.Context, path string, dest any) error {
	return c.requestJSON(ctx, http.MethodGet, path, nil, dest)
}

// postJSON performs a POST request with a JSON body and decodes the response.
func (c *Client) postJSON(ctx context.Context, path string, payload, dest any) error {
	return c.requestJSON(ctx, http.MethodPost, path, payload, dest)
}

func (c *Client) requestJSON(ctx context.Context, method, path string, payload, dest any) error {
	return googleoauth.JSON(ctx, c.doAPI, method, path, payload, dest, googleoauth.APIOptions{
		Service:          "Gmail",
		MaxResponseBytes: maxAPIResponseBytes,
	})
}
