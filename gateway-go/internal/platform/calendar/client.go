package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/googleoauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// tokenURL is the Google OAuth2 endpoint; overridable in tests.
var tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential

// apiBase is the Calendar v3 root for the authenticated user's primary calendar.
const apiBase = "https://www.googleapis.com/calendar/v3"

// Response-size bounds — same rationale as the Gmail client: cap every external
// body read so a runaway upstream can't balloon the always-on gateway's memory.
// Calendar payloads are event lists (far smaller than mail), so 16 MiB is ample.
// Readers fetch limit+1 and fail with an explicit over-limit error.
const maxAPIResponseBytes = 16 << 20

// setTokenURL overrides the token endpoint URL (for testing).
func setTokenURL(u string) { tokenURL = u }

// tokenJSON matches Google's OAuth2 token storage shape.
type tokenJSON = googleoauth.Token

// Client is a Calendar API client with auto-refreshing OAuth2 tokens.
// The credential/token file pair lives separately from Gmail's to keep
// scopes orthogonal — same Google Cloud project, different OAuth flow.
type Client struct {
	tokens     *googleoauth.Source
	httpClient *http.Client
}

var (
	globalMu     sync.Mutex
	globalClient *Client
)

// googleCalendarEnabled reports whether the read-only Google Calendar integration
// is on. It is OPT-IN (default off): the Deneb calendar is local-only unless
// DENEB_CALENDAR_GOOGLE is truthy. A Google list is a ~270ms API round-trip on
// every cold calendar load while the local store is instant, so the integration
// is off by default; set DENEB_CALENDAR_GOOGLE=1 (with credentials present) to
// re-enable. All callers already degrade to the local store when this errors.
func googleCalendarEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_CALENDAR_GOOGLE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// DefaultClient returns the singleton Calendar client, initializing on
// first call. Mirrors gmail.DefaultClient: failed init can be retried.
// Returns an error (so every caller falls back to the local calendar) when the
// Google integration is disabled — the default; see googleCalendarEnabled.
func DefaultClient() (*Client, error) {
	if !googleCalendarEnabled() {
		return nil, fmt.Errorf("Google Calendar 연동이 꺼져 있습니다 — 로컬 캘린더만 사용 (DENEB_CALENDAR_GOOGLE=1 로 활성화)") //nolint:staticcheck // ST1005 — Korean operator-facing message
	}

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

	loaded, err := googleoauth.Load("Calendar", clientPath, tokenPath)
	if err != nil {
		return nil, err
	}

	httpClient := httputil.NewClient(60 * time.Second)
	return &Client{
		tokens:     googleoauth.NewSource("Calendar", loaded, tokenPath, httpClient),
		httpClient: httpClient,
	}, nil
}

// validToken returns the current access token, refreshing if needed.
func (c *Client) validToken(ctx context.Context) (string, error) {
	return c.tokens.ValidToken(ctx, tokenURL)
}

func (c *Client) doAPI(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return googleoauth.DoBearer(ctx, c.httpClient, c.validToken, apiBase, method, path, body)
}

func (c *Client) readJSON(ctx context.Context, path string, dest any) error {
	return googleoauth.JSON(ctx, c.doAPI, http.MethodGet, path, nil, dest, googleoauth.APIOptions{
		Service:          "Calendar",
		MaxResponseBytes: maxAPIResponseBytes,
		StatusError: func(statusCode int, body string) error {
			return &APIError{StatusCode: statusCode, Body: body}
		},
	})
}

// APIError carries Calendar API HTTP failures so callers can branch on
// the status code (e.g. 404 → NOT_FOUND) instead of substring-matching
// the message body. Implements error so wrapped through errors.As.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns the human-readable error message.
func (e *APIError) Error() string {
	return fmt.Sprintf("Calendar API 오류 (HTTP %d): %s", e.StatusCode, e.Body) //nolint:staticcheck // ST1005 — Korean error message
}
